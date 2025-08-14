package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	httpv1alpha1 "github.com/konnektr-io/http-query-operator/api/v1alpha1"
	"github.com/konnektr-io/http-query-operator/internal/util"
)

const (
	ManagedByLabel         = "konnektr.io/managed-by" // Label to identify managed resources
	ControllerName         = "httpqueryresource-controller"
	ConditionReconciled    = "Reconciled"
	ConditionHTTPConnected = "HTTPConnected"
	HTTPQueryFinalizer     = "konnektr.io/httpqueryresource-finalizer"
)

// HTTPQueryResourceReconciler reconciles an HTTPQueryResource object
type HTTPQueryResourceReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Log               logr.Logger
	HTTPClientFactory func(ctx context.Context) (util.HTTPClient, error)
	OwnedGVKs         []schema.GroupVersionKind
	AuthResolver      *util.AuthResolver
	TemplateProcessor *util.TemplateProcessor
}

// Key for context value to indicate child resource event
var childResourceEventKey = struct{}{}

// Info about the triggering child resource
type childResourceInfo struct {
	GVK       schema.GroupVersionKind
	Namespace string
	Name      string
}

//+kubebuilder:rbac:groups=konnektr.io,resources=httpqueryresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=konnektr.io,resources=httpqueryresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=konnektr.io,resources=httpqueryresources/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *HTTPQueryResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	r.Log = log
	log.Info("Reconciling HTTPQueryResource", "Request.Namespace", req.Namespace, "Request.Name", req.Name)

	// Check if this reconciliation was triggered by a child resource
	childInfo, isChildEvent := ctx.Value(childResourceEventKey).(*childResourceInfo)
	if isChildEvent {
		log.Info("Reconciliation triggered by child resource event",
			"child-gvk", childInfo.GVK.String(),
			"child-name", childInfo.Name,
			"child-namespace", childInfo.Namespace)
	}

	// Fetch the HTTPQueryResource instance
	httpQueryResource := &httpv1alpha1.HTTPQueryResource{}
	err := r.Get(ctx, req.NamespacedName, httpQueryResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("HTTPQueryResource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get HTTPQueryResource")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if httpQueryResource.GetDeletionTimestamp() != nil {
		return r.handleDeletion(ctx, httpQueryResource)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(httpQueryResource, HTTPQueryFinalizer) {
		controllerutil.AddFinalizer(httpQueryResource, HTTPQueryFinalizer)
		if err := r.Update(ctx, httpQueryResource); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Initialize HTTP client
	httpClient, err := r.HTTPClientFactory(ctx)
	if err != nil {
		log.Error(err, "Failed to create HTTP client")
		r.setCondition(httpQueryResource, ConditionHTTPConnected, metav1.ConditionFalse, "HTTPClientError", err.Error())
		if updateErr := r.Status().Update(ctx, httpQueryResource); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Set HTTP connected condition
	r.setCondition(httpQueryResource, ConditionHTTPConnected, metav1.ConditionTrue, "HTTPClientConnected", "HTTP client successfully initialized")

	// Execute the reconciliation
	result, err := r.reconcileResources(ctx, httpQueryResource, httpClient)

	// Update conditions based on result
	if err != nil {
		log.Error(err, "Failed to reconcile resources")
		r.setCondition(httpQueryResource, ConditionReconciled, metav1.ConditionFalse, "ReconciliationError", err.Error())
	} else {
		r.setCondition(httpQueryResource, ConditionReconciled, metav1.ConditionTrue, "ReconciliationSuccessful", "Resources successfully reconciled")
	}

	// Update status
	if statusErr := r.Status().Update(ctx, httpQueryResource); statusErr != nil {
		log.Error(statusErr, "Failed to update status")
		return ctrl.Result{}, statusErr
	}

	// Handle poll interval for requeue
	if httpQueryResource.Spec.PollInterval != "" {
		duration, parseErr := time.ParseDuration(httpQueryResource.Spec.PollInterval)
		if parseErr != nil {
			log.Error(parseErr, "Invalid poll interval format", "interval", httpQueryResource.Spec.PollInterval)
			return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
		}
		log.V(1).Info("Scheduling next reconciliation", "interval", duration)
		result.RequeueAfter = duration
	}

	return result, err
}

// handleDeletion handles cleanup when an HTTPQueryResource is being deleted
func (r *HTTPQueryResourceReconciler) handleDeletion(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource) (ctrl.Result, error) {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)
	log.Info("Handling deletion of HTTPQueryResource")

	// Delete all managed resources
	if err := r.deleteOwnedResources(ctx, httpQueryResource); err != nil {
		log.Error(err, "Failed to delete owned resources")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(httpQueryResource, HTTPQueryFinalizer)
	if err := r.Update(ctx, httpQueryResource); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Successfully deleted HTTPQueryResource")
	return ctrl.Result{}, nil
}

// setCondition sets or updates a condition on the HTTPQueryResource status
func (r *HTTPQueryResourceReconciler) setCondition(httpQueryResource *httpv1alpha1.HTTPQueryResource, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Find existing condition
	for i, existingCondition := range httpQueryResource.Status.Conditions {
		if existingCondition.Type == conditionType {
			// Only update if status changed
			if existingCondition.Status != status {
				httpQueryResource.Status.Conditions[i] = condition
			} else {
				// Update message and reason even if status is the same
				httpQueryResource.Status.Conditions[i].Message = message
				httpQueryResource.Status.Conditions[i].Reason = reason
			}
			return
		}
	}

	// Add new condition
	httpQueryResource.Status.Conditions = append(httpQueryResource.Status.Conditions, condition)
}

// reconcileResources performs the main reconciliation logic
func (r *HTTPQueryResourceReconciler) reconcileResources(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource, httpClient util.HTTPClient) (ctrl.Result, error) {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)

	// Create HTTP config from HTTPQueryResource
	httpConfig := util.HTTPConfig{
		URL:          httpQueryResource.Spec.HTTP.URL,
		Method:       httpQueryResource.Spec.HTTP.Method,
		Headers:      httpQueryResource.Spec.HTTP.Headers,
		Body:         httpQueryResource.Spec.HTTP.Body,
		ResponsePath: httpQueryResource.Spec.HTTP.ResponsePath,
	}

	// Set authentication config if provided
	if httpQueryResource.Spec.HTTP.AuthenticationRef != nil {
		// Initialize AuthResolver if not set
		if r.AuthResolver == nil {
			r.AuthResolver = util.NewAuthResolver(r.Client, r.Log)
		}

		authConfig, err := r.AuthResolver.ResolveAuthenticationConfig(ctx, httpQueryResource.Namespace, httpQueryResource.Spec.HTTP.AuthenticationRef)
		if err != nil {
			log.Error(err, "Failed to resolve authentication configuration")
			return ctrl.Result{}, err
		}
		httpConfig.AuthType = authConfig.AuthType
		httpConfig.AuthConfig = authConfig.AuthConfig
	}

	// Execute HTTP request
	log.Info("Executing HTTP request", "url", httpConfig.URL)
	items, err := httpClient.Execute(ctx, httpConfig)
	if err != nil {
		log.Error(err, "Failed to execute HTTP request")
		return ctrl.Result{}, err
	}

	// Process response and apply resources
	resources, err := r.processHTTPResponse(ctx, httpQueryResource, items)
	if err != nil {
		log.Error(err, "Failed to process HTTP response")
		return ctrl.Result{}, err
	}

	// Apply the resources to the cluster
	for _, resource := range resources {
		if err := r.applyResource(ctx, httpQueryResource, resource); err != nil {
			log.Error(err, "Failed to apply resource", "resource", resource.GetName())
			return ctrl.Result{}, err
		}
	}

	// Clean up resources that are no longer in the response
	if httpQueryResource.Spec.Prune != nil && *httpQueryResource.Spec.Prune {
		if err := r.cleanupUnmanagedResources(ctx, httpQueryResource, resources); err != nil {
			log.Error(err, "Failed to cleanup unmanaged resources")
			return ctrl.Result{}, err
		}
	}

	// Set Status.ManagedResources to the names of all managed resources (sorted)
	managedResourceNames := make([]string, 0, len(resources))
	for _, resource := range resources {
		managedResourceNames = append(managedResourceNames, resource.GetName())
	}
	// Sort for consistency
	if len(managedResourceNames) > 1 {
		sort.Strings(managedResourceNames)
	}
	httpQueryResource.Status.ManagedResources = managedResourceNames

	// Execute status update callbacks for managed resources if configured
	if httpQueryResource.Spec.StatusUpdate != nil {
		if err := r.updateStatusForChildResources(ctx, httpQueryResource, resources, httpClient); err != nil {
			log.Error(err, "Failed to execute status updates for child resources")
			// Don't fail reconciliation for status update errors
		}
	}

	log.Info("Successfully reconciled HTTPQueryResource", "resourceCount", len(resources))
	return ctrl.Result{}, nil
}

// processHTTPResponse processes the HTTP response and converts it to Kubernetes resources
func (r *HTTPQueryResourceReconciler) processHTTPResponse(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource, items []util.ItemResult) ([]*unstructured.Unstructured, error) {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)

	// Initialize TemplateProcessor if not set
	if r.TemplateProcessor == nil {
		r.TemplateProcessor = util.NewTemplateProcessor()
	}

	// Use TemplateProcessor to process items into resources
	resources, err := r.TemplateProcessor.ProcessHTTPResponseToResources(httpQueryResource.Spec.Template, items)
	if err != nil {
		return nil, err
	}

	log.Info("Processed HTTP response", "itemCount", len(items), "resourceCount", len(resources))
	return resources, nil
}

// applyResource applies a single resource to the cluster
func (r *HTTPQueryResourceReconciler) applyResource(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource, resource *unstructured.Unstructured) error {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name, "resource", resource.GetName())

	// Set namespace if not specified and HTTPQueryResource is namespaced
	if resource.GetNamespace() == "" && httpQueryResource.GetNamespace() != "" {
		resource.SetNamespace(httpQueryResource.GetNamespace())
	}

	// Add owner reference
	if err := controllerutil.SetControllerReference(httpQueryResource, resource, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Add managed-by label
	labels := resource.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ManagedByLabel] = ControllerName
	resource.SetLabels(labels)

	// Try to get existing resource
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(resource.GetAPIVersion())
	existing.SetKind(resource.GetKind())

	err := r.Get(ctx, types.NamespacedName{
		Name:      resource.GetName(),
		Namespace: resource.GetNamespace(),
	}, existing)

	if err != nil && apierrors.IsNotFound(err) {
		// Create new resource
		log.Info("Creating new resource")
		if err := r.Create(ctx, resource); err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get existing resource: %w", err)
	} else {
		// Update existing resource
		log.Info("Updating existing resource")
		resource.SetResourceVersion(existing.GetResourceVersion())
		if err := r.Update(ctx, resource); err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	}

	return nil
}

// deleteOwnedResources deletes all resources owned by the HTTPQueryResource
func (r *HTTPQueryResourceReconciler) deleteOwnedResources(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource) error {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)

	// List all resources with our managed-by label
	for _, gvk := range r.OwnedGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(gvk.GroupVersion().String())
		list.SetKind(gvk.Kind + "List")

		listOpts := []client.ListOption{
			client.InNamespace(httpQueryResource.GetNamespace()),
			client.MatchingLabels{ManagedByLabel: ControllerName},
		}

		if err := r.List(ctx, list, listOpts...); err != nil {
			log.Error(err, "Failed to list owned resources", "gvk", gvk.String())
			continue
		}

		for _, item := range list.Items {
			// Check if this resource is owned by our HTTPQueryResource
			for _, ownerRef := range item.GetOwnerReferences() {
				if ownerRef.UID == httpQueryResource.GetUID() {
					log.Info("Deleting owned resource", "resource", item.GetName(), "gvk", gvk.String())
					if err := r.Delete(ctx, &item); err != nil && !apierrors.IsNotFound(err) {
						log.Error(err, "Failed to delete owned resource", "resource", item.GetName())
					}
					break
				}
			}
		}
	}

	return nil
}

// cleanupUnmanagedResources removes resources that are no longer managed
func (r *HTTPQueryResourceReconciler) cleanupUnmanagedResources(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource, currentResources []*unstructured.Unstructured) error {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)

	// Create a set of current resource names for quick lookup
	currentResourceNames := make(map[string]bool)
	for _, resource := range currentResources {
		key := fmt.Sprintf("%s/%s/%s", resource.GetAPIVersion(), resource.GetKind(), resource.GetName())
		currentResourceNames[key] = true
	}

	// List all resources with our managed-by label
	for _, gvk := range r.OwnedGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(gvk.GroupVersion().String())
		list.SetKind(gvk.Kind + "List")

		listOpts := []client.ListOption{
			client.InNamespace(httpQueryResource.GetNamespace()),
			client.MatchingLabels{ManagedByLabel: ControllerName},
		}

		if err := r.List(ctx, list, listOpts...); err != nil {
			log.Error(err, "Failed to list managed resources", "gvk", gvk.String())
			continue
		}

		for _, item := range list.Items {
			// Check if this resource is owned by our HTTPQueryResource
			isOwned := false
			for _, ownerRef := range item.GetOwnerReferences() {
				if ownerRef.UID == httpQueryResource.GetUID() {
					isOwned = true
					break
				}
			}

			if !isOwned {
				continue
			}

			// Check if this resource is still in the current set
			key := fmt.Sprintf("%s/%s/%s", item.GetAPIVersion(), item.GetKind(), item.GetName())
			if !currentResourceNames[key] {
				log.Info("Deleting unmanaged resource", "resource", item.GetName(), "gvk", gvk.String())
				if err := r.Delete(ctx, &item); err != nil && !apierrors.IsNotFound(err) {
					log.Error(err, "Failed to delete unmanaged resource", "resource", item.GetName())
				}
			}
		}
	}

	return nil
}

// SetupWithManagerAndGVKs sets up the controller with the Manager and watches specific GVKs.
func (r *HTTPQueryResourceReconciler) SetupWithManagerAndGVKs(mgr ctrl.Manager, ownedGVKs []schema.GroupVersionKind) error {
	r.OwnedGVKs = ownedGVKs // Store the GVKs for use in reconciliation

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&httpv1alpha1.HTTPQueryResource{})

	// Custom event handler for owned resources
	for _, gvk := range ownedGVKs {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		controllerBuilder = controllerBuilder.Owns(u, builder.WithPredicates(
			statusChangePredicate(),
			predicate.ResourceVersionChangedPredicate{},
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))
	}

	return controllerBuilder.Complete(r)
}

// statusChangePredicate returns a predicate that triggers on various resource changes
func statusChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Always trigger on update (including status changes)
			return true
		},
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

// updateStatusForChildResources sends status updates for managed resources
func (r *HTTPQueryResourceReconciler) updateStatusForChildResources(ctx context.Context, httpQueryResource *httpv1alpha1.HTTPQueryResource, resources []*unstructured.Unstructured, httpClient util.HTTPClient) error {
	log := r.Log.WithValues("httpqueryresource", httpQueryResource.Name)

	if httpQueryResource.Spec.StatusUpdate == nil {
		return nil
	}

	// Prepare base status config
	statusConfig := util.HTTPStatusUpdateConfig{
		URL:          httpQueryResource.Spec.StatusUpdate.URL,
		Method:       httpQueryResource.Spec.StatusUpdate.Method,
		Headers:      httpQueryResource.Spec.StatusUpdate.Headers,
		BodyTemplate: httpQueryResource.Spec.StatusUpdate.BodyTemplate,
	}

	// Resolve authentication for status updates
	if httpQueryResource.Spec.StatusUpdate.AuthenticationRef != nil {
		// Initialize AuthResolver if not set
		if r.AuthResolver == nil {
			r.AuthResolver = util.NewAuthResolver(r.Client, r.Log)
		}

		authConfig, err := r.AuthResolver.ResolveAuthenticationConfig(ctx, httpQueryResource.Namespace, httpQueryResource.Spec.StatusUpdate.AuthenticationRef)
		if err != nil {
			log.Error(err, "Failed to resolve status update authentication configuration")
			r.setCondition(httpQueryResource, ConditionReconciled, metav1.ConditionFalse, "StatusUpdateFailed", "Failed to resolve status update authentication configuration: "+err.Error())
			_ = r.Status().Update(ctx, httpQueryResource)
			return err
		}
		statusConfig.AuthType = authConfig.AuthType
		statusConfig.AuthConfig = authConfig.AuthConfig
	}

	// Send status updates for each managed resource and track errors
	hadError := false
	for _, resource := range resources {
		// Get the current resource from the cluster to have the latest status
		currentResource := &unstructured.Unstructured{}
		currentResource.SetGroupVersionKind(resource.GroupVersionKind())

		err := r.Get(ctx, types.NamespacedName{
			Name:      resource.GetName(),
			Namespace: resource.GetNamespace(),
		}, currentResource)

		if err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("Resource not found for status update, skipping", "resource", resource.GetName())
				continue
			}
			log.Error(err, "Failed to get current resource for status update", "resource", resource.GetName())
			hadError = true
			continue
		}

		// Extract original item data from annotations
		var originalItem map[string]interface{}
		annotations := currentResource.GetAnnotations()
		if annotations != nil {
			if itemJSON, exists := annotations["konnektr.io/original-item"]; exists {
				if err := json.Unmarshal([]byte(itemJSON), &originalItem); err != nil {
					log.Error(err, "Failed to unmarshal original item data", "resource", resource.GetName())
					originalItem = make(map[string]interface{})
				}
			}
		}

		// Create enhanced template context with both resource and original item
		templateData := map[string]interface{}{
			"Resource": currentResource.Object,
			"Item":     originalItem,
		}

		// Execute status update with enhanced context
		if err := httpClient.ExecuteStatusUpdate(ctx, statusConfig, templateData); err != nil {
			log.Error(err, "Failed to execute status update for resource", "resource", resource.GetName())
			hadError = true
			continue
		}

		log.V(1).Info("Successfully sent status update for resource", "resource", resource.GetName())
	}

	// Set and persist a status condition on the parent CR
	if hadError {
		r.setCondition(httpQueryResource, ConditionReconciled, metav1.ConditionFalse, "StatusUpdateFailed", "One or more status update callbacks failed")
	} else {
		r.setCondition(httpQueryResource, ConditionReconciled, metav1.ConditionTrue, "StatusUpdateSuccess", "All status update callbacks succeeded")
	}
	_ = r.Status().Update(ctx, httpQueryResource)

	if hadError {
		return fmt.Errorf("one or more status update callbacks failed")
	}

	return nil
}

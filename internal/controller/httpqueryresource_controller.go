package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	httpv1alpha1 "github.com/konnektr-io/http-query-operator/api/v1alpha1"
	"github.com/konnektr-io/http-query-operator/internal/util"
)

const (
	ManagedByLabel         = "konnektr.io/managed-by"
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

	// Fetch the HTTPQueryResource instance
	httpqr := &httpv1alpha1.HTTPQueryResource{}
	if err := r.Get(ctx, req.NamespacedName, httpqr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("HTTPQueryResource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get HTTPQueryResource")
		return ctrl.Result{}, err
	}

	// Parse Poll Interval
	pollInterval, err := time.ParseDuration(httpqr.Spec.PollInterval)
	if err != nil {
		log.Error(err, "Invalid pollInterval format")
		return ctrl.Result{}, nil
	}

	// TODO: Implement HTTP request logic here
	log.Info("HTTPQueryResource reconciled successfully", "PollInterval", pollInterval)

	// Requeue after the poll interval
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HTTPQueryResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&httpv1alpha1.HTTPQueryResource{}).
		Complete(r)
}

// SetupWithManagerAndGVKs sets up the controller with the Manager and watches specific GVKs.
func (r *HTTPQueryResourceReconciler) SetupWithManagerAndGVKs(mgr ctrl.Manager, gvks []schema.GroupVersionKind) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&httpv1alpha1.HTTPQueryResource{}).
		Complete(r)
}

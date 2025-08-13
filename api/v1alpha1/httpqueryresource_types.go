// +kubebuilder:object:generate=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HTTPAuthenticationRef defines how to authenticate HTTP requests via a Secret.
type HTTPAuthenticationRef struct {
	// Name of the Secret containing authentication details.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace of the Secret. Defaults to the namespace of the HTTPQueryResource.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Type of authentication. Supported: basic, bearer, apikey
	// +kubebuilder:validation:Enum=basic;bearer;apikey
	// +kubebuilder:validation:Required
	Type string `json:"type"`
	// Key within the Secret for the username (basic auth). Defaults to "username".
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`
	// Key within the Secret for the password (basic auth). Defaults to "password".
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
	// Key within the Secret for the token (bearer auth). Defaults to "token".
	// +optional
	TokenKey string `json:"tokenKey,omitempty"`
	// Key within the Secret for the API key. Defaults to "apikey".
	// +optional
	APIKeyKey string `json:"apikeyKey,omitempty"`
	// Header name for API key authentication. Defaults to "X-API-Key".
	// +optional
	APIKeyHeader string `json:"apikeyHeader,omitempty"`
}

// HTTPSpec defines the HTTP request details.
type HTTPSpec struct {
	// URL for the HTTP request.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^https?://.+"
	URL string `json:"url"`
	// HTTP method. Defaults to GET.
	// +kubebuilder:validation:Enum=GET;POST;PUT;PATCH;DELETE
	// +kubebuilder:default=GET
	// +optional
	Method string `json:"method,omitempty"`
	// HTTP headers to include in the request.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
	// Request body for POST/PUT/PATCH requests. Can be a Go template.
	// +optional
	Body string `json:"body,omitempty"`
	// Authentication details.
	// +optional
	AuthenticationRef *HTTPAuthenticationRef `json:"authenticationRef,omitempty"`
	// JSONPath expression to extract array data from response. Defaults to "$" (root).
	// Use this when the API response is not directly an array.
	// Example: "$.data" if response is {"data": [...]}
	// +optional
	ResponsePath string `json:"responsePath,omitempty"`
}

// HTTPStatusUpdateSpec defines how to update status via HTTP requests.
type HTTPStatusUpdateSpec struct {
	// URL for the status update HTTP request. Can be a Go template.
	// +kubebuilder:validation:Required
	URL string `json:"url"`
	// HTTP method for status updates. Defaults to PATCH.
	// +kubebuilder:validation:Enum=POST;PUT;PATCH;DELETE
	// +kubebuilder:default=PATCH
	// +optional
	Method string `json:"method,omitempty"`
	// HTTP headers to include in the status update request.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
	// Go template for the request body. Receives the resource data.
	// +kubebuilder:validation:Required
	BodyTemplate string `json:"bodyTemplate"`
	// Authentication details for status updates.
	// +optional
	AuthenticationRef *HTTPAuthenticationRef `json:"authenticationRef,omitempty"`
}

// HTTPQueryResourceSpec defines the desired state of HTTPQueryResource
// +kubebuilder:deepcopy-gen=true
type HTTPQueryResourceSpec struct {
	// PollInterval defines how often to make the HTTP request and reconcile resources.
	// Format is a duration string like "5m", "1h", "30s".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	PollInterval string `json:"pollInterval"`

	// HTTP request details.
	// +kubebuilder:validation:Required
	HTTP HTTPSpec `json:"http"`

	// Go template string for the Kubernetes resource to be created for each item.
	// The template will receive a map[string]interface{} named `Item` representing the JSON object.
	// Field names are the keys in the map.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// Prune determines if resources previously created by this CR but no longer corresponding
	// to an item in the latest HTTP response should be deleted. Defaults to true.
	// +optional
	// +kubebuilder:default=true
	Prune *bool `json:"prune,omitempty"`

	// StatusUpdate defines how to update status via HTTP requests.
	// +kubebuilder:validation:Optional
	StatusUpdate *HTTPStatusUpdateSpec `json:"statusUpdate,omitempty"`
}

// HTTPQueryResourceStatus defines the observed state of HTTPQueryResource
// +kubebuilder:deepcopy-gen=true
type HTTPQueryResourceStatus struct {
	// Conditions represent the latest available observations of the resource's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastPollTime records when the HTTP endpoint was last successfully queried.
	// +optional
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`

	// ManagedResources lists the resources currently managed by this CR.
	// +optional
	ManagedResources []string `json:"managedResources,omitempty"`

	// ObservedGeneration reflects the generation of the CR spec that was last processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Interval",type="string",JSONPath=".spec.pollInterval",description="Polling interval"
//+kubebuilder:printcolumn:name="Last Poll",type="date",JSONPath=".status.lastPollTime",description="Last successful poll time"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// HTTPQueryResource is the Schema for the httpqueryresources API
type HTTPQueryResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HTTPQueryResourceSpec   `json:"spec,omitempty"`
	Status HTTPQueryResourceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HTTPQueryResourceList contains a list of HTTPQueryResource
type HTTPQueryResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HTTPQueryResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HTTPQueryResource{}, &HTTPQueryResourceList{})
}

// Helper function to get default prune value
func (s *HTTPQueryResourceSpec) GetPrune() bool {
	if s.Prune == nil {
		return true // Default to true if not set
	}
	return *s.Prune
}

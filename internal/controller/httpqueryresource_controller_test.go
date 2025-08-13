package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	httpv1alpha1 "github.com/konnektr-io/http-query-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("HTTPQueryResource controller", func() {
	const (
		ResourceNamespace = "default"
		SecretName        = "test-http-secret"
		timeout           = time.Second * 30
		interval          = time.Millisecond * 250
	)

	Describe("When reconciling an HTTPQueryResource", func() {
		It("Should create a ConfigMap with correct labels and data from mock HTTP API", func() {
			ctx := context.Background()

			// Create a mock HTTP server
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				response := `[
					{
						"id": 1,
						"username": "testuser",
						"email": "test@example.com",
						"name": "Test User"
					}
				]`
				w.Write([]byte(response))
			}))
			defer mockServer.Close()

			// Create the HTTPQueryResource
			hqr := &httpv1alpha1.HTTPQueryResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mock-hqr",
					Namespace: ResourceNamespace,
				},
				Spec: httpv1alpha1.HTTPQueryResourceSpec{
					PollInterval: "10s",
					Prune:        ptrBool(true),
					HTTP: httpv1alpha1.HTTPSpec{
						URL:    mockServer.URL,
						Method: "GET",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						ResponsePath: "$",
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm-{{ .Item.id }}
  namespace: default
data:
  username: "{{ .Item.username }}"
  email: "{{ .Item.email }}"
  name: "{{ .Item.name }}"`,
				},
			}
			Expect(k8sClient.Create(ctx, hqr)).To(Succeed())

			// Check the HTTPQueryResource is created and reconciled
			lookupKey := types.NamespacedName{Name: "mock-hqr", Namespace: ResourceNamespace}
			created := &httpv1alpha1.HTTPQueryResource{}
			By("Checking the HTTPQueryResource is created and reconciled")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookupKey, created)).To(Succeed())
				g.Expect(created.Status.Conditions).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			// Check the ConfigMap is created with correct labels and data
			cmName := "test-cm-1"
			cmLookup := types.NamespacedName{Name: cmName, Namespace: ResourceNamespace}
			createdCM := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
				labels := createdCM.GetLabels()
				g.Expect(labels).To(HaveKeyWithValue(ManagedByLabel, ControllerName))
				// Assert ConfigMap data
				g.Expect(createdCM.Data).To(HaveKeyWithValue("username", "testuser"))
				g.Expect(createdCM.Data).To(HaveKeyWithValue("email", "test@example.com"))
				g.Expect(createdCM.Data).To(HaveKeyWithValue("name", "Test User"))
			}, timeout, interval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, hqr)).To(Succeed())
		})

		// Test multiple items with advanced templating and pruning
		It("should create/update resources for each item and handle pruning correctly", func() {
			ctx := context.Background()

			// Create a mock HTTP server with configurable responses
			mockServer := NewMockHTTPServer()
			defer mockServer.Close()

			// Set initial response with multiple users
			initialResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body: `[
					{"id": 1, "username": "alice", "email": "alice@example.com", "role": "admin"},
					{"id": 2, "username": "bob", "email": "bob@example.com", "role": "user"},
					{"id": 3, "username": "charlie", "email": "charlie@example.com", "role": "user"}
				]`,
			}
			mockServer.SetResponse("/users", initialResponse)

			// Create the HTTPQueryResource with advanced templating
			hqr := &httpv1alpha1.HTTPQueryResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-users-hqr",
					Namespace: ResourceNamespace,
				},
				Spec: httpv1alpha1.HTTPQueryResourceSpec{
					PollInterval: "10s",
					Prune:        ptrBool(true),
					HTTP: httpv1alpha1.HTTPSpec{
						URL:    mockServer.URL() + "/users",
						Method: "GET",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						ResponsePath: "$",
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: user-{{ .Item.username | lower }}
  namespace: default
  labels:
    {{- if eq .Item.role "admin" }}
    is-admin: "true"
    {{- else }}
    is-admin: "false"
    {{- end }}
    user-role: {{ .Item.role }}
data:
  user-id: "{{ .Item.id }}"
  username: "{{ .Item.username }}"
  email: "{{ .Item.email }}"
  role: "{{ .Item.role }}"
  display-name: "{{ .Item.username | title }}"
  created-at: "{{ now | date "2006-01-02T15:04:05Z07:00" }}"`,
				},
			}
			Expect(k8sClient.Create(ctx, hqr)).To(Succeed())

			// Wait for all ConfigMaps to be created
			usernames := []string{"alice", "bob", "charlie"}
			for _, username := range usernames {
				cmName := "user-" + username
				cmLookup := types.NamespacedName{Name: cmName, Namespace: ResourceNamespace}
				createdCM := &corev1.ConfigMap{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
					labels := createdCM.GetLabels()
					g.Expect(labels).To(HaveKeyWithValue(ManagedByLabel, ControllerName))

					// Check advanced templating worked
					if username == "alice" {
						g.Expect(labels).To(HaveKeyWithValue("is-admin", "true"))
						g.Expect(labels).To(HaveKeyWithValue("user-role", "admin"))
					} else {
						g.Expect(labels).To(HaveKeyWithValue("is-admin", "false"))
						g.Expect(labels).To(HaveKeyWithValue("user-role", "user"))
					}

					// Check data
					g.Expect(createdCM.Data).To(HaveKeyWithValue("username", username))
					g.Expect(createdCM.Data).To(HaveKeyWithValue("display-name", cases.Title(language.English).String(username)))
				}, timeout, interval).Should(Succeed())
			}

			// Now update the mock server response - remove charlie, keep alice and bob
			updatedResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body: `[
					{"id": 1, "username": "alice", "email": "alice@example.com", "role": "admin"},
					{"id": 2, "username": "bob", "email": "bob.updated@example.com", "role": "moderator"}
				]`,
			}
			mockServer.SetResponse("/users", updatedResponse)

			// Wait for the next poll cycle (should prune charlie's ConfigMap)
			time.Sleep(12 * time.Second)

			// Alice and Bob should still exist
			for _, username := range []string{"alice", "bob"} {
				cmName := "user-" + username
				cmLookup := types.NamespacedName{Name: cmName, Namespace: ResourceNamespace}
				createdCM := &corev1.ConfigMap{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
					if username == "bob" {
						// Bob's email should be updated and role changed
						g.Expect(createdCM.Data).To(HaveKeyWithValue("email", "bob.updated@example.com"))
						labels := createdCM.GetLabels()
						g.Expect(labels).To(HaveKeyWithValue("user-role", "moderator"))
					}
				}, timeout, interval).Should(Succeed())
			}

			// Charlie should be deleted due to pruning
			charlieClookup := types.NamespacedName{Name: "user-charlie", Namespace: ResourceNamespace}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, charlieClookup, &corev1.ConfigMap{})
				g.Expect(err).To(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, hqr)).To(Succeed())
		})

		// Test basic authentication
		It("should authenticate HTTP requests using bearer token authentication", func() {
			ctx := context.Background()

			// Create a mock HTTP server that requires authentication
			mockServer := NewMockHTTPServer()
			defer mockServer.Close()

			// Set response that checks for Authorization header
			authResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body: `[
					{"id": 1, "username": "authenticated-user", "email": "auth@example.com"}
				]`,
			}
			mockServer.SetResponse("/api/data", authResponse)

			// Create authentication secret
			authSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bearer-auth-secret",
					Namespace: ResourceNamespace,
				},
				Data: map[string][]byte{
					"token": []byte("test-bearer-token-12345"),
				},
			}
			Expect(k8sClient.Create(ctx, authSecret)).To(Succeed())

			// Create the HTTPQueryResource with bearer authentication
			hqr := &httpv1alpha1.HTTPQueryResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-test-hqr",
					Namespace: ResourceNamespace,
				},
				Spec: httpv1alpha1.HTTPQueryResourceSpec{
					PollInterval: "10s",
					Prune:        ptrBool(true),
					HTTP: httpv1alpha1.HTTPSpec{
						URL:    mockServer.URL() + "/api/data",
						Method: "GET",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						ResponsePath: "$",
						AuthenticationRef: &httpv1alpha1.HTTPAuthenticationRef{
							Name: "bearer-auth-secret",
							Type: "bearer",
						},
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: auth-test-{{ .Item.id }}
  namespace: default
data:
  username: "{{ .Item.username }}"
  email: "{{ .Item.email }}"
  authenticated: "true"`,
				},
			}
			Expect(k8sClient.Create(ctx, hqr)).To(Succeed())

			// Wait for the ConfigMap to be created
			cmLookup := types.NamespacedName{Name: "auth-test-1", Namespace: ResourceNamespace}
			createdCM := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
				g.Expect(createdCM.Data).To(HaveKeyWithValue("username", "authenticated-user"))
				g.Expect(createdCM.Data).To(HaveKeyWithValue("authenticated", "true"))
			}, timeout, interval).Should(Succeed())

			// Verify that the authentication header was sent
			Eventually(func(g Gomega) {
				requests := mockServer.GetRequests()
				g.Expect(requests).NotTo(BeEmpty())

				found := false
				for _, req := range requests {
					if authHeader, exists := req.Headers["Authorization"]; exists {
						g.Expect(authHeader).To(Equal("Bearer test-bearer-token-12345"))
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(), "Authorization header should be present in requests")
			}, timeout, interval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, hqr)).To(Succeed())
			Expect(k8sClient.Delete(ctx, authSecret)).To(Succeed())
		})

		It("should send status updates to HTTP endpoint when resource status changes", func() {
			// Create a mock HTTP server with configurable responses
			mockServer := NewMockHTTPServer()
			defer mockServer.Close()

			// Set up response for the main data request
			mainResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body: `[
					{"id": 1, "username": "alice", "email": "alice@example.com", "role": "admin"},
					{"id": 2, "username": "bob", "email": "bob@example.com", "role": "user"}
				]`,
			}
			mockServer.SetResponse("/users", mainResponse)

			// Set up response for status update endpoint
			statusUpdateResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       `{"status": "updated"}`,
			}
			mockServer.SetResponse("/status-updates", statusUpdateResponse)

			// Create the HTTPQueryResource with status update configuration
			hqr := &httpv1alpha1.HTTPQueryResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "status-update-hqr",
					Namespace: ResourceNamespace,
				},
				Spec: httpv1alpha1.HTTPQueryResourceSpec{
					PollInterval: "10s",
					HTTP: httpv1alpha1.HTTPSpec{
						URL:    mockServer.URL() + "/users",
						Method: "GET",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						ResponsePath: "$",
					},
					StatusUpdate: &httpv1alpha1.HTTPStatusUpdateSpec{
						URL:    mockServer.URL() + "/status-updates",
						Method: "POST",
						Headers: map[string]string{
							"Content-Type": "application/json",
						},
						BodyTemplate: `{
  "resource_name": "{{ .Resource.metadata.name }}",
  "resource_kind": "{{ .Resource.kind }}",
  "original_item": {{ .Item | toJson }},
  "timestamp": "{{ now | date "2006-01-02T15:04:05Z07:00" }}"
}`,
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: user-{{ .Item.username | lower }}
  namespace: default
data:
  user-id: "{{ .Item.id }}"
  username: "{{ .Item.username }}"
  email: "{{ .Item.email }}"
  role: "{{ .Item.role }}"`,
				},
			}
			Expect(k8sClient.Create(ctx, hqr)).To(Succeed())

			// Wait for ConfigMaps to be created
			usernames := []string{"alice", "bob"}
			for _, username := range usernames {
				cmName := "user-" + username
				cmLookup := types.NamespacedName{Name: cmName, Namespace: ResourceNamespace}
				createdCM := &corev1.ConfigMap{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
					labels := createdCM.GetLabels()
					g.Expect(labels).To(HaveKeyWithValue(ManagedByLabel, ControllerName))
				}, timeout, interval).Should(Succeed())
			}

			// Wait for status update requests to be sent
			Eventually(func(g Gomega) {
				requests := mockServer.GetRequests()
				
				// Filter for status update requests (POST to /status-updates)
				statusUpdateRequests := []MockRequest{}
				for _, req := range requests {
					if req.Method == "POST" && strings.Contains(req.URL, "/status-updates") {
						statusUpdateRequests = append(statusUpdateRequests, req)
					}
				}
				
				// Should have at least 2 status update requests (one for each user)
				g.Expect(len(statusUpdateRequests)).To(BeNumerically(">=", 2))
				
				// Verify the request structure
				for _, req := range statusUpdateRequests {
					// Check headers
					g.Expect(req.Headers).To(HaveKeyWithValue("Content-Type", "application/json"))
					
					// Parse and validate the request body
					var body map[string]interface{}
					g.Expect(json.Unmarshal([]byte(req.Body), &body)).To(Succeed())
					
					// Verify template was processed correctly
					g.Expect(body).To(HaveKey("resource_name"))
					g.Expect(body).To(HaveKey("resource_kind"))
					g.Expect(body).To(HaveKey("original_item"))
					g.Expect(body).To(HaveKey("timestamp"))
					
					// Verify resource information
					g.Expect(body["resource_kind"]).To(Equal("ConfigMap"))
					
					// Verify original item contains expected user data
					originalItem, ok := body["original_item"].(map[string]interface{})
					g.Expect(ok).To(BeTrue(), "original_item should be an object")
					g.Expect(originalItem).To(HaveKey("username"))
					g.Expect(originalItem).To(HaveKey("email"))
					g.Expect(originalItem).To(HaveKey("role"))
					
					// Verify timestamp format
					timestamp, ok := body["timestamp"].(string)
					g.Expect(ok).To(BeTrue(), "timestamp should be a string")
					_, err := time.Parse("2006-01-02T15:04:05Z07:00", timestamp)
					g.Expect(err).ToNot(HaveOccurred(), "timestamp should be in RFC3339 format")
				}
			}, timeout, interval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, hqr)).To(Succeed())
		})

		It("should extract data from nested JSON responses using JSONPath", func() {
			ctx := context.Background()

			// Create a mock HTTP server with nested JSON structure
			mockServer := NewMockHTTPServer()
			defer mockServer.Close()

			// Set up response with nested data structure
			nestedResponse := MockResponse{
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body: `{
					"status": "success",
					"metadata": {
						"total": 3,
						"page": 1
					},
					"data": {
						"users": [
							{
								"id": 10,
								"profile": {
									"username": "nested-alice",
									"contact": {
										"email": "alice@nested.com"
									}
								},
								"settings": {
									"theme": "dark",
									"notifications": true
								}
							},
							{
								"id": 20,
								"profile": {
									"username": "nested-bob", 
									"contact": {
										"email": "bob@nested.com"
									}
								},
								"settings": {
									"theme": "light",
									"notifications": false
								}
							},
							{
								"id": 30,
								"profile": {
									"username": "nested-charlie",
									"contact": {
										"email": "charlie@nested.com"
									}
								},
								"settings": {
									"theme": "auto",
									"notifications": true
								}
							}
						]
					}
				}`,
			}
			mockServer.SetResponse("/api/nested", nestedResponse)

			// Create the HTTPQueryResource with JSONPath to extract nested array
			hqr := &httpv1alpha1.HTTPQueryResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jsonpath-hqr",
					Namespace: ResourceNamespace,
				},
				Spec: httpv1alpha1.HTTPQueryResourceSpec{
					PollInterval: "10s",
					Prune:        ptrBool(true),
					HTTP: httpv1alpha1.HTTPSpec{
						URL:    mockServer.URL() + "/api/nested",
						Method: "GET",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						ResponsePath: "data.users",
					},
					Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: nested-user-{{ .Item.id }}
  namespace: default
  labels:
    theme: {{ .Item.settings.theme }}
    notifications: "{{ .Item.settings.notifications }}"
data:
  user-id: "{{ .Item.id }}"
  username: "{{ .Item.profile.username }}"
  email: "{{ .Item.profile.contact.email }}"
  theme: "{{ .Item.settings.theme }}"
  notifications: "{{ .Item.settings.notifications }}"
  full-profile: '{{ .Item | toJson }}'`,
				},
			}
			Expect(k8sClient.Create(ctx, hqr)).To(Succeed())

			// Wait for all ConfigMaps to be created from the nested data
			expectedUsers := []struct {
				id       int
				username string
				email    string
				theme    string
				notify   bool
			}{
				{10, "nested-alice", "alice@nested.com", "dark", true},
				{20, "nested-bob", "bob@nested.com", "light", false},
				{30, "nested-charlie", "charlie@nested.com", "auto", true},
			}

			for _, user := range expectedUsers {
				cmName := "nested-user-" + toString(user.id)
				cmLookup := types.NamespacedName{Name: cmName, Namespace: ResourceNamespace}
				createdCM := &corev1.ConfigMap{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmLookup, createdCM)).To(Succeed())
					
					// Verify labels from nested data
					labels := createdCM.GetLabels()
					g.Expect(labels).To(HaveKeyWithValue(ManagedByLabel, ControllerName))
					g.Expect(labels).To(HaveKeyWithValue("theme", user.theme))
					g.Expect(labels).To(HaveKeyWithValue("notifications", toString(user.notify)))
					
					// Verify data extracted from deeply nested fields
					g.Expect(createdCM.Data).To(HaveKeyWithValue("user-id", toString(user.id)))
					g.Expect(createdCM.Data).To(HaveKeyWithValue("username", user.username))
					g.Expect(createdCM.Data).To(HaveKeyWithValue("email", user.email))
					g.Expect(createdCM.Data).To(HaveKeyWithValue("theme", user.theme))
					g.Expect(createdCM.Data).To(HaveKeyWithValue("notifications", toString(user.notify)))
					
					// Verify the full profile JSON is stored correctly
					g.Expect(createdCM.Data).To(HaveKey("full-profile"))
					var fullProfile map[string]interface{}
					g.Expect(json.Unmarshal([]byte(createdCM.Data["full-profile"]), &fullProfile)).To(Succeed())
					g.Expect(fullProfile).To(HaveKeyWithValue("id", float64(user.id))) // JSON numbers become float64
					g.Expect(fullProfile).To(HaveKey("profile"))
					g.Expect(fullProfile).To(HaveKey("settings"))
				}, timeout, interval).Should(Succeed())
			}

			// Verify that exactly 3 ConfigMaps were created (one for each user in the nested array)
			Eventually(func(g Gomega) {
				lookupKey := types.NamespacedName{Name: "jsonpath-hqr", Namespace: ResourceNamespace}
				created := &httpv1alpha1.HTTPQueryResource{}
				g.Expect(k8sClient.Get(ctx, lookupKey, created)).To(Succeed())
				g.Expect(created.Status.ManagedResources).To(HaveLen(3))
			}, timeout, interval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, hqr)).To(Succeed())
		})

		// TODO: Placeholder for error handling test
		XIt("should handle HTTP errors and update status conditions appropriately", func() {
			// This test will verify:
			// - 404, 500, timeout errors are handled gracefully
			// - Status conditions reflect error states
			// - Retry behavior works correctly
		})

		// TODO: Placeholder for template error test
		XIt("should handle template rendering errors gracefully", func() {
			// This test will verify:
			// - Invalid templates don't crash the controller
			// - Template execution errors are reported in status
			// - Partial failures don't affect other items
		})
	})

	Describe("HTTPQueryResource finalizer cleanup logic", func() {
		// TODO: Placeholder for finalizer test
		XIt("should delete managed resources and remove the finalizer when the CR is deleted and the finalizer is set", func() {
			// This test will verify:
			// - Finalizer prevents deletion until cleanup is complete
			// - All managed resources are deleted during cleanup
			// - Finalizer is removed after successful cleanup
		})
	})

	Describe("HTTPQueryResource deployment management", func() {
		// TODO: Placeholder for deployment test
		XIt("should create Deployments and send status updates based on deployment readiness", func() {
			// This test will verify:
			// - Can create Deployment resources from API data
			// - Status updates are sent when deployment becomes ready
			// - Template context includes both original item and resource status
		})
	})

	Describe("HTTPQueryResource with OAuth2 authentication", func() {
		// TODO: Placeholder for OAuth2 test
		XIt("should authenticate using OAuth2 client credentials flow", func() {
			// This test will verify:
			// - OAuth2 token acquisition works
			// - Tokens are included in HTTP requests
			// - Token refresh works when tokens expire
			// - Multiple scopes can be requested
		})
	})
})

// MockHTTPServer provides a configurable HTTP server for testing
type MockHTTPServer struct {
	server     *httptest.Server
	responses  map[string]MockResponse
	requests   []MockRequest
	authHeader string
	mu         sync.RWMutex
}

type MockResponse struct {
	StatusCode int
	Body       string
	Headers    map[string]string
}

type MockRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

func NewMockHTTPServer() *MockHTTPServer {
	mock := &MockHTTPServer{
		responses: make(map[string]MockResponse),
		requests:  []MockRequest{},
	}

	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleRequest(w, r)
	}))

	return mock
}

func (m *MockHTTPServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Read request body
	body := ""
	if r.Body != nil {
		if bodyBytes, err := io.ReadAll(r.Body); err == nil {
			body = string(bodyBytes)
		}
	}

	// Record the request
	req := MockRequest{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: make(map[string]string),
		Body:    body,
	}
	for k, v := range r.Header {
		if len(v) > 0 {
			req.Headers[k] = v[0]
		}
	}
	m.requests = append(m.requests, req)

	// Find response for this path
	path := r.URL.Path
	if response, exists := m.responses[path]; exists {
		// Set headers
		for k, v := range response.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(response.StatusCode)
		w.Write([]byte(response.Body))
	} else {
		// Default 404
		w.WriteHeader(404)
		w.Write([]byte(`{"error": "not found"}`))
	}
}

func (m *MockHTTPServer) SetResponse(path string, response MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[path] = response
}

func (m *MockHTTPServer) GetRequests() []MockRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	requests := make([]MockRequest, len(m.requests))
	copy(requests, m.requests)
	return requests
}

func (m *MockHTTPServer) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = []MockRequest{}
}

func (m *MockHTTPServer) URL() string {
	return m.server.URL
}

func (m *MockHTTPServer) Close() {
	m.server.Close()
}

// Helper for pointer to bool
func ptrBool(b bool) *bool { return &b }

// Helper for string conversion
func toString(val interface{}) string {
	switch v := val.(type) {
	case int:
		return strconv.Itoa(v)
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

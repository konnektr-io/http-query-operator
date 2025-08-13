package util

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	httpv1alpha1 "github.com/konnektr-io/http-query-operator/api/v1alpha1"
)

func TestAuthResolver_ResolveAuthenticationConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name      string
		secret    *corev1.Secret
		authRef   *httpv1alpha1.HTTPAuthenticationRef
		namespace string
		expected  *ResolvedAuthConfig
		wantErr   bool
	}{
		{
			name: "basic auth with default keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("testuser"),
					"password": []byte("testpass"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "test-secret",
				Type: "basic",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "basic",
				AuthConfig: map[string]string{
					"username": "testuser",
					"password": "testpass",
				},
			},
			wantErr: false,
		},
		{
			name: "basic auth with custom keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"user": []byte("testuser"),
					"pass": []byte("testpass"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name:        "test-secret",
				Type:        "basic",
				UsernameKey: "user",
				PasswordKey: "pass",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "basic",
				AuthConfig: map[string]string{
					"username": "testuser",
					"password": "testpass",
				},
			},
			wantErr: false,
		},
		{
			name: "bearer token auth",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("bearer-token-123"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "test-secret",
				Type: "bearer",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "bearer",
				AuthConfig: map[string]string{
					"token": "bearer-token-123",
				},
			},
			wantErr: false,
		},
		{
			name: "api key auth with default header",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"apikey": []byte("api-key-456"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "test-secret",
				Type: "apikey",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "apikey",
				AuthConfig: map[string]string{
					"apikey": "api-key-456",
					"header": "X-API-Key",
				},
			},
			wantErr: false,
		},
		{
			name: "api key auth with custom header",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"apikey": []byte("api-key-789"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name:         "test-secret",
				Type:         "apikey",
				APIKeyHeader: "Authorization",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "apikey",
				AuthConfig: map[string]string{
					"apikey": "api-key-789",
					"header": "Authorization",
				},
			},
			wantErr: false,
		},
		{
			name: "oauth2 auth",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"clientId":     []byte("client123"),
					"clientSecret": []byte("secret456"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name:     "test-secret",
				Type:     "oauth2",
				TokenURL: "https://auth.example.com/token",
				Scopes:   "read write",
			},
			namespace: "default",
			expected: &ResolvedAuthConfig{
				AuthType: "oauth2",
				AuthConfig: map[string]string{
					"clientId":     "client123",
					"clientSecret": "secret456",
					"tokenUrl":     "https://auth.example.com/token",
					"scopes":       "read write",
				},
			},
			wantErr: false,
		},
		{
			name: "secret not found",
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "nonexistent-secret",
				Type: "basic",
			},
			namespace: "default",
			expected:  nil,
			wantErr:   true,
		},
		{
			name: "oauth2 missing token url",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"clientId":     []byte("client123"),
					"clientSecret": []byte("secret456"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "test-secret",
				Type: "oauth2",
				// Missing TokenURL
			},
			namespace: "default",
			expected:  nil,
			wantErr:   true,
		},
		{
			name: "unsupported auth type",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("some-token"),
				},
			},
			authRef: &httpv1alpha1.HTTPAuthenticationRef{
				Name: "test-secret",
				Type: "unsupported",
			},
			namespace: "default",
			expected:  nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			var objects []client.Object
			if tt.secret != nil {
				objects = append(objects, tt.secret)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create auth resolver
			resolver := NewAuthResolver(fakeClient, logr.Discard())

			// Test resolution
			result, err := resolver.ResolveAuthenticationConfig(context.Background(), tt.namespace, tt.authRef)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.AuthType, result.AuthType)
				assert.Equal(t, tt.expected.AuthConfig, result.AuthConfig)
			}
		})
	}
}

func TestAuthResolver_DifferentNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "other-namespace",
		},
		Data: map[string][]byte{
			"token": []byte("test-token"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	resolver := NewAuthResolver(fakeClient, logr.Discard())

	authRef := &httpv1alpha1.HTTPAuthenticationRef{
		Name:      "test-secret",
		Namespace: "other-namespace", // Explicitly specify different namespace
		Type:      "bearer",
	}

	result, err := resolver.ResolveAuthenticationConfig(context.Background(), "default", authRef)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "bearer", result.AuthType)
	assert.Equal(t, "test-token", result.AuthConfig["token"])
}

package util

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	httpv1alpha1 "github.com/konnektr-io/http-query-operator/api/v1alpha1"
)

// ResolvedAuthConfig holds the resolved authentication configuration
type ResolvedAuthConfig struct {
	AuthType   string
	AuthConfig map[string]string
}

// AuthResolver handles resolving authentication configuration from Kubernetes secrets
type AuthResolver struct {
	Client client.Client
	Log    logr.Logger
}

// NewAuthResolver creates a new AuthResolver
func NewAuthResolver(client client.Client, log logr.Logger) *AuthResolver {
	return &AuthResolver{
		Client: client,
		Log:    log,
	}
}

// ResolveAuthenticationConfig resolves authentication credentials from Kubernetes secrets
func (ar *AuthResolver) ResolveAuthenticationConfig(ctx context.Context, namespace string, authRef *httpv1alpha1.HTTPAuthenticationRef) (*ResolvedAuthConfig, error) {
	log := ar.Log.WithValues("authRef", authRef.Name)

	// Determine secret namespace (default to provided namespace)
	secretNamespace := authRef.Namespace
	if secretNamespace == "" {
		secretNamespace = namespace
	}

	// Fetch the secret
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      authRef.Name,
		Namespace: secretNamespace,
	}

	if err := ar.Client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("authentication secret '%s' not found in namespace '%s'", authRef.Name, secretNamespace)
		}
		return nil, fmt.Errorf("failed to get authentication secret '%s' in namespace '%s': %w", authRef.Name, secretNamespace, err)
	}

	// Helper function to get value from secret with default key
	getValue := func(specifiedKey, defaultKey string) string {
		key := specifiedKey
		if key == "" {
			key = defaultKey
		}
		if value, exists := secret.Data[key]; exists {
			return string(value)
		}
		return ""
	}

	authConfig := &ResolvedAuthConfig{
		AuthType:   authRef.Type,
		AuthConfig: make(map[string]string),
	}

	switch authRef.Type {
	case "basic":
		username := getValue(authRef.UsernameKey, "username")
		password := getValue(authRef.PasswordKey, "password")
		authConfig.AuthConfig["username"] = username
		authConfig.AuthConfig["password"] = password

		if username == "" && password == "" {
			log.Info("Warning: Basic auth configured but no credentials found in secret", "secret", authRef.Name)
		}

	case "bearer":
		token := getValue(authRef.TokenKey, "token")
		authConfig.AuthConfig["token"] = token

		if token == "" {
			log.Info("Warning: Bearer auth configured but no token found in secret", "secret", authRef.Name)
		}

	case "apikey":
		apiKey := getValue(authRef.APIKeyKey, "apikey")
		header := authRef.APIKeyHeader
		if header == "" {
			header = "X-API-Key"
		}
		authConfig.AuthConfig["apikey"] = apiKey
		authConfig.AuthConfig["header"] = header

		if apiKey == "" {
			log.Info("Warning: API key auth configured but no API key found in secret", "secret", authRef.Name)
		}

	case "oauth2":
		clientID := getValue(authRef.ClientIDKey, "clientId")
		clientSecret := getValue(authRef.ClientSecretKey, "clientSecret")
		tokenURL := authRef.TokenURL
		scopes := authRef.Scopes

		authConfig.AuthConfig["clientId"] = clientID
		authConfig.AuthConfig["clientSecret"] = clientSecret
		authConfig.AuthConfig["tokenUrl"] = tokenURL
		authConfig.AuthConfig["scopes"] = scopes

		if clientID == "" || clientSecret == "" || tokenURL == "" {
			return nil, fmt.Errorf("OAuth2 authentication requires clientId, clientSecret in secret and tokenUrl in spec")
		}

	default:
		return nil, fmt.Errorf("unsupported authentication type: %s", authRef.Type)
	}

	log.V(1).Info("Successfully resolved authentication configuration", "type", authRef.Type, "secret", authRef.Name)
	return authConfig, nil
}

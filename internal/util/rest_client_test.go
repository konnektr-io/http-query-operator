package util

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRESTClient_parseResponse(t *testing.T) {
	client := NewRESTClient()

	tests := []struct {
		name         string
		body         string
		responsePath string
		expected     []ItemResult
		wantErr      bool
	}{
		{
			name:         "array response at root",
			body:         `[{"id": 1, "name": "test1"}, {"id": 2, "name": "test2"}]`,
			responsePath: "",
			expected: []ItemResult{
				{"id": float64(1), "name": "test1"},
				{"id": float64(2), "name": "test2"},
			},
			wantErr: false,
		},
		{
			name:         "array response with path",
			body:         `{"data": [{"id": 1, "name": "test1"}, {"id": 2, "name": "test2"}]}`,
			responsePath: "data",
			expected: []ItemResult{
				{"id": float64(1), "name": "test1"},
				{"id": float64(2), "name": "test2"},
			},
			wantErr: false,
		},
		{
			name:         "single object response",
			body:         `{"id": 1, "name": "test1"}`,
			responsePath: "",
			expected: []ItemResult{
				{"id": float64(1), "name": "test1"},
			},
			wantErr: false,
		},
		{
			name:         "nested object response",
			body:         `{"result": {"id": 1, "name": "test1"}}`,
			responsePath: "result",
			expected: []ItemResult{
				{"id": float64(1), "name": "test1"},
			},
			wantErr: false,
		},
		{
			name:         "empty array",
			body:         `[]`,
			responsePath: "",
			expected:     []ItemResult{},
			wantErr:      false,
		},
		{
			name:         "path not found",
			body:         `{"other": "data"}`,
			responsePath: "nonexistent",
			expected:     nil,
			wantErr:      true,
		},
		{
			name:         "invalid json",
			body:         `{invalid json`,
			responsePath: "$",
			expected:     nil,
			wantErr:      true,
		},
		{
			name:         "path points to primitive value",
			body:         `{"message": "hello"}`,
			responsePath: "$.message",
			expected:     nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.parseResponse([]byte(tt.body), tt.responsePath)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestRESTClient_addAuthentication(t *testing.T) {
	client := NewRESTClient()

	tests := []struct {
		name         string
		authType     string
		authConfig   map[string]string
		expectHeader func(t *testing.T, req *http.Request)
		wantErr      bool
	}{
		{
			name:     "basic auth",
			authType: "basic",
			authConfig: map[string]string{
				"username": "testuser",
				"password": "testpass",
			},
			expectHeader: func(t *testing.T, req *http.Request) {
				username, password, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "testuser", username)
				assert.Equal(t, "testpass", password)
			},
			wantErr: false,
		},
		{
			name:     "bearer token",
			authType: "bearer",
			authConfig: map[string]string{
				"token": "test-token-123",
			},
			expectHeader: func(t *testing.T, req *http.Request) {
				authHeader := req.Header.Get("Authorization")
				assert.Equal(t, "Bearer test-token-123", authHeader)
			},
			wantErr: false,
		},
		{
			name:     "api key with default header",
			authType: "apikey",
			authConfig: map[string]string{
				"apikey": "test-api-key",
			},
			expectHeader: func(t *testing.T, req *http.Request) {
				apiKeyHeader := req.Header.Get("X-API-Key")
				assert.Equal(t, "test-api-key", apiKeyHeader)
			},
			wantErr: false,
		},
		{
			name:     "api key with custom header",
			authType: "apikey",
			authConfig: map[string]string{
				"apikey": "test-api-key",
				"header": "Custom-API-Key",
			},
			expectHeader: func(t *testing.T, req *http.Request) {
				apiKeyHeader := req.Header.Get("Custom-API-Key")
				assert.Equal(t, "test-api-key", apiKeyHeader)
			},
			wantErr: false,
		},
		{
			name:       "no auth",
			authType:   "",
			authConfig: map[string]string{},
			expectHeader: func(t *testing.T, req *http.Request) {
				// No specific headers should be set
				assert.Empty(t, req.Header.Get("Authorization"))
				assert.Empty(t, req.Header.Get("X-API-Key"))
			},
			wantErr: false,
		},
		{
			name:       "unsupported auth type",
			authType:   "unsupported",
			authConfig: map[string]string{},
			expectHeader: func(t *testing.T, req *http.Request) {
				// Should not be called
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://example.com", nil)
			require.NoError(t, err)

			err = client.addAuthentication(req, tt.authType, tt.authConfig)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tt.expectHeader(t, req)
			}
		})
	}
}

func TestRESTClient_Execute_Integration(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check method and headers
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		// Return test data
		response := map[string]interface{}{
			"users": []map[string]interface{}{
				{"id": 1, "name": "Alice"},
				{"id": 2, "name": "Bob"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewRESTClient()
	config := HTTPConfig{
		URL:    server.URL,
		Method: "GET",
		Headers: map[string]string{
			"Accept": "application/json",
		},
		ResponsePath: "users",
	}

	items, err := client.Execute(context.Background(), config)
	assert.NoError(t, err)
	require.Len(t, items, 2)

	assert.Equal(t, float64(1), items[0]["id"])
	assert.Equal(t, "Alice", items[0]["name"])
	assert.Equal(t, float64(2), items[1]["id"])
	assert.Equal(t, "Bob", items[1]["name"])
}

func TestRESTClient_Execute_Error_Cases(t *testing.T) {
	client := NewRESTClient()

	tests := []struct {
		name        string
		serverFunc  http.HandlerFunc
		config      HTTPConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "404 error",
			serverFunc: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Not Found"))
			}),
			config: HTTPConfig{
				URL:          "", // Will be set to server URL
				Method:       "GET",
				ResponsePath: "$",
			},
			wantErr:     true,
			errContains: "404",
		},
		{
			name: "invalid JSON response",
			serverFunc: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("invalid json"))
			}),
			config: HTTPConfig{
				URL:          "", // Will be set to server URL
				Method:       "GET",
				ResponsePath: "",
			},
			wantErr:     true,
			errContains: "valid json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverFunc)
			defer server.Close()

			tt.config.URL = server.URL

			items, err := client.Execute(context.Background(), tt.config)
			assert.Error(t, err)
			assert.Nil(t, items)
			if tt.errContains != "" {
				assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.errContains))
			}
		})
	}
}

func TestRESTClient_ExecuteStatusUpdate(t *testing.T) {
	// Create a test server
	receivedBody := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewRESTClient()
	config := HTTPStatusUpdateConfig{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BodyTemplate: `{"resource_name": "{{ .Resource.name }}", "status": "updated"}`,
	}

	resource := map[string]interface{}{
		"name": "test-resource",
	}

	err := client.ExecuteStatusUpdate(context.Background(), config, resource)

	assert.NoError(t, err)
	assert.Contains(t, receivedBody, "test-resource")
	assert.Contains(t, receivedBody, "updated")
}

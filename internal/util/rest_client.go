package util

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/tidwall/gjson"
	"golang.org/x/oauth2/clientcredentials"
)

// RESTClient implements HTTPClient for REST APIs.
type RESTClient struct {
	client *http.Client
}

// NewRESTClient creates a new REST client.
func NewRESTClient() *RESTClient {
	return &RESTClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Execute performs an HTTP request and returns the response items.
func (r *RESTClient) Execute(ctx context.Context, config HTTPConfig) ([]ItemResult, error) {
	req, err := r.buildRequest(ctx, config.URL, config.Method, config.Headers, config.Body, config.AuthType, config.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return r.parseResponse(bodyBytes, config.ResponsePath)
}

// ExecuteStatusUpdate performs an HTTP request to update resource status.
func (r *RESTClient) ExecuteStatusUpdate(ctx context.Context, config HTTPStatusUpdateConfig, resource interface{}) error {
	// Render the body template
	tmpl, err := template.New("statusUpdate").Funcs(sprig.TxtFuncMap()).Parse(config.BodyTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse body template: %w", err)
	}

	var bodyBuffer bytes.Buffer
	// Use the full resource data as template context (which should contain both Resource and Item)
	err = tmpl.Execute(&bodyBuffer, resource)
	if err != nil {
		return fmt.Errorf("failed to render body template: %w", err)
	}

	// Render the URL template
	urlTmpl, err := template.New("statusUpdateURL").Funcs(sprig.TxtFuncMap()).Parse(config.URL)
	if err != nil {
		return fmt.Errorf("failed to parse URL template: %w", err)
	}

	var urlBuffer bytes.Buffer
	// Use the full resource data as template context
	err = urlTmpl.Execute(&urlBuffer, resource)
	if err != nil {
		return fmt.Errorf("failed to render URL template: %w", err)
	}

	req, err := r.buildRequest(ctx, urlBuffer.String(), config.Method, config.Headers, bodyBuffer.String(), config.AuthType, config.AuthConfig)
	if err != nil {
		return fmt.Errorf("failed to build status update request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("status update HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status update HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildRequest constructs an HTTP request with authentication.
func (r *RESTClient) buildRequest(ctx context.Context, url, method string, headers map[string]string, body, authType string, authConfig map[string]string) (*http.Request, error) {
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Set Content-Type for body requests
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add authentication
	err = r.addAuthentication(req, authType, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to add authentication: %w", err)
	}

	return req, nil
}

// addAuthentication adds authentication to the request.
func (r *RESTClient) addAuthentication(req *http.Request, authType string, authConfig map[string]string) error {
	switch strings.ToLower(authType) {
	case "basic":
		username := authConfig["username"]
		password := authConfig["password"]
		if username != "" || password != "" {
			req.SetBasicAuth(username, password)
		}
	case "bearer":
		token := authConfig["token"]
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case "apikey":
		apiKey := authConfig["apikey"]
		header := authConfig["header"]
		if header == "" {
			header = "X-API-Key"
		}
		if apiKey != "" {
			req.Header.Set(header, apiKey)
		}
	case "oauth2":
		// For OAuth2, we need to get a token using client credentials flow
		token, err := r.getOAuth2Token(req.Context(), authConfig)
		if err != nil {
			return fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case "":
		// No authentication
	default:
		return fmt.Errorf("unsupported authentication type: %s", authType)
	}
	return nil
}

// getOAuth2Token performs OAuth2 client credentials flow to get an access token.
func (r *RESTClient) getOAuth2Token(ctx context.Context, authConfig map[string]string) (string, error) {
	clientID := authConfig["clientId"]
	clientSecret := authConfig["clientSecret"]
	tokenURL := authConfig["tokenUrl"]
	scopes := authConfig["scopes"]

	if clientID == "" || clientSecret == "" || tokenURL == "" {
		return "", fmt.Errorf("OAuth2 requires clientId, clientSecret, and tokenUrl")
	}

	// Configure OAuth2 client credentials
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
	}

	// Parse scopes if provided
	if scopes != "" {
		config.Scopes = strings.Fields(scopes)
	}

	// Get token
	token, err := config.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve OAuth2 token: %w", err)
	}

	return token.AccessToken, nil
}

// parseResponse extracts items from the HTTP response.
func (r *RESTClient) parseResponse(body []byte, responsePath string) ([]ItemResult, error) {
	if responsePath == "" || responsePath == "$" {
		// For root path, we need to parse the entire JSON
		if len(body) == 0 {
			return []ItemResult{}, nil
		}
		
		// Check if it's an array at root
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
			items := []ItemResult{}
			var rawItems []map[string]interface{}
			if err := json.Unmarshal(body, &rawItems); err != nil {
				return nil, fmt.Errorf("failed to parse JSON array: %w", err)
			}
			for _, item := range rawItems {
				items = append(items, ItemResult(item))
			}
			return items, nil
		}
		
		// Check if it's an object at root
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
			var item map[string]interface{}
			if err := json.Unmarshal(body, &item); err != nil {
				return nil, fmt.Errorf("failed to parse JSON object: %w", err)
			}
			return []ItemResult{ItemResult(item)}, nil
		}
		
		return nil, fmt.Errorf("response is not a valid JSON object or array")
	}

	// Use gjson to extract data
	result := gjson.GetBytes(body, responsePath)
	if !result.Exists() {
		return nil, fmt.Errorf("response path '%s' not found in response", responsePath)
	}

	var items []ItemResult = []ItemResult{}

	if result.IsArray() {
		// Response path points to an array
		result.ForEach(func(key, value gjson.Result) bool {
			var item ItemResult
			if err := json.Unmarshal([]byte(value.Raw), &item); err == nil {
				items = append(items, item)
			}
			return true
		})
	} else if result.IsObject() {
		// Response path points to a single object
		var item ItemResult
		if err := json.Unmarshal([]byte(result.Raw), &item); err == nil {
			items = append(items, item)
		}
	} else {
		return nil, fmt.Errorf("response path '%s' does not point to an object or array", responsePath)
	}

	return items, nil
}

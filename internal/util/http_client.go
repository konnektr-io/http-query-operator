package util

import (
	"context"
)

// ItemResult represents a single item from an HTTP response.
type ItemResult map[string]interface{}

// HTTPClient abstracts HTTP operations for the controller.
type HTTPClient interface {
	Execute(ctx context.Context, config HTTPConfig) ([]ItemResult, error)
	ExecuteStatusUpdate(ctx context.Context, config HTTPStatusUpdateConfig, resource interface{}) error
}

// HTTPConfig represents the configuration for HTTP requests.
type HTTPConfig struct {
	URL               string
	Method            string
	Headers           map[string]string
	Body              string
	AuthType          string
	AuthConfig        map[string]string
	ResponsePath      string
}

// HTTPStatusUpdateConfig represents the configuration for HTTP status update requests.
type HTTPStatusUpdateConfig struct {
	URL          string
	Method       string
	Headers      map[string]string
	BodyTemplate string
	AuthType     string
	AuthConfig   map[string]string
}

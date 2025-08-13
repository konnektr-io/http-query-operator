package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// TemplateProcessor handles template processing and resource parsing
type TemplateProcessor struct{}

// NewTemplateProcessor creates a new TemplateProcessor
func NewTemplateProcessor() *TemplateProcessor {
	return &TemplateProcessor{}
}

// ProcessTemplate processes a Go template with the given data
func (tp *TemplateProcessor) ProcessTemplate(templateStr string, data interface{}) (string, error) {
	tmpl, err := template.New("resource").Funcs(sprig.TxtFuncMap()).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// ParseResources parses YAML/JSON string into Kubernetes resources
func (tp *TemplateProcessor) ParseResources(data string) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured

	// Split by YAML document separator
	docs := strings.Split(data, "---")
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Try to parse as JSON first, then YAML
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(doc), &obj); err != nil {
			if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
				return nil, fmt.Errorf("failed to parse document as JSON or YAML: %w", err)
			}
		}

		if len(obj) == 0 {
			continue
		}

		resource := &unstructured.Unstructured{Object: obj}
		resources = append(resources, resource)
	}

	return resources, nil
}

// ProcessHTTPResponseToResources processes HTTP response items into Kubernetes resources
func (tp *TemplateProcessor) ProcessHTTPResponseToResources(templateStr string, items []ItemResult) ([]*unstructured.Unstructured, error) {
	var allResources []*unstructured.Unstructured

	// Process each item from the HTTP response
	for i, item := range items {
		// Template data includes the item data and index
		templateData := map[string]interface{}{
			"Item":  item,
			"Index": i,
		}

		// Process the template
		renderedYAML, err := tp.ProcessTemplate(templateStr, templateData)
		if err != nil {
			return nil, fmt.Errorf("failed to process template for item %d: %w", i, err)
		}

		// Parse the generated YAML/JSON into Kubernetes resources
		itemResources, err := tp.ParseResources(renderedYAML)
		if err != nil {
			return nil, fmt.Errorf("failed to parse resources for item %d: %w", i, err)
		}

		// Add metadata to track the original item data for status updates
		itemJSON, _ := json.Marshal(item)
		for _, resource := range itemResources {
			// Add annotation with original item data
			annotations := resource.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["konnektr.io/original-item"] = string(itemJSON)
			resource.SetAnnotations(annotations)
		}

		allResources = append(allResources, itemResources...)
	}

	return allResources, nil
}

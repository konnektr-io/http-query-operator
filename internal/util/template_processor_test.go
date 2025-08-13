package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateProcessor_ProcessTemplate(t *testing.T) {
	tp := NewTemplateProcessor()

	tests := []struct {
		name     string
		template string
		data     interface{}
		expected string
		wantErr  bool
	}{
		{
			name:     "simple template",
			template: "Hello {{ .Name }}!",
			data:     map[string]interface{}{"Name": "World"},
			expected: "Hello World!",
			wantErr:  false,
		},
		{
			name:     "template with sprig functions",
			template: "{{ .Name | upper }}-{{ .Index | toString }}",
			data:     map[string]interface{}{"Name": "test", "Index": 42},
			expected: "TEST-42",
			wantErr:  false,
		},
		{
			name:     "template with nested data",
			template: "User: {{ .User.Name }} ({{ .User.Email }})",
			data: map[string]interface{}{
				"User": map[string]interface{}{
					"Name":  "Alice",
					"Email": "alice@example.com",
				},
			},
			expected: "User: Alice (alice@example.com)",
			wantErr:  false,
		},
		{
			name:     "invalid template syntax",
			template: "{{ .Name",
			data:     map[string]interface{}{"Name": "World"},
			expected: "",
			wantErr:  true,
		},
		{
			name:     "template execution error - invalid function",
			template: "{{ .Name | invalidFunction }}",
			data:     map[string]interface{}{"Name": "World"},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tp.ProcessTemplate(tt.template, tt.data)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTemplateProcessor_ParseResources(t *testing.T) {
	tp := NewTemplateProcessor()

	tests := []struct {
		name          string
		data          string
		expectedCount int
		wantErr       bool
	}{
		{
			name: "single YAML resource",
			data: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`,
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name: "multiple YAML resources",
			data: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm1
data:
  key: value1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm2
data:
  key: value2`,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name: "JSON resource",
			data: `{
  "apiVersion": "v1",
  "kind": "Secret",
  "metadata": {"name": "test-secret"},
  "data": {"key": "dmFsdWU="}
}`,
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name: "empty documents ignored",
			data: `---

apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value

---
`,
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name:          "invalid YAML/JSON",
			data:          `invalid: yaml: content: [`,
			expectedCount: 0,
			wantErr:       true,
		},
		{
			name:          "empty input",
			data:          "",
			expectedCount: 0,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := tp.ParseResources(tt.data)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, resources, tt.expectedCount)

				// Verify each resource has basic fields
				for _, resource := range resources {
					assert.NotEmpty(t, resource.GetAPIVersion())
					assert.NotEmpty(t, resource.GetKind())
				}
			}
		})
	}
}

func TestTemplateProcessor_ProcessHTTPResponseToResources(t *testing.T) {
	tp := NewTemplateProcessor()

	template := `apiVersion: v1
kind: ConfigMap
metadata:
  name: user-{{ .Item.id }}
  namespace: default
data:
  username: "{{ .Item.username }}"
  email: "{{ .Item.email }}"`

	items := []ItemResult{
		{
			"id":       1,
			"username": "alice",
			"email":    "alice@example.com",
		},
		{
			"id":       2,
			"username": "bob",
			"email":    "bob@example.com",
		},
	}

	resources, err := tp.ProcessHTTPResponseToResources(template, items)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	// Check first resource
	assert.Equal(t, "v1", resources[0].GetAPIVersion())
	assert.Equal(t, "ConfigMap", resources[0].GetKind())
	assert.Equal(t, "user-1", resources[0].GetName())

	// Check that original item data is stored in annotations
	annotations := resources[0].GetAnnotations()
	assert.Contains(t, annotations, "konnektr.io/original-item")
	assert.Contains(t, annotations["konnektr.io/original-item"], "alice")

	// Check second resource
	assert.Equal(t, "user-2", resources[1].GetName())
	annotations2 := resources[1].GetAnnotations()
	assert.Contains(t, annotations2, "konnektr.io/original-item")
	assert.Contains(t, annotations2["konnektr.io/original-item"], "bob")
}

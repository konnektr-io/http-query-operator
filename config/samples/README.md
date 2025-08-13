# Sample Configurations

This directory contains several sample `HTTPQueryResource` configurations demonstrating different features of the HTTP Query Operator.

## Quick Start - Simple Test

For quick testing and development:

```bash
kubectl apply -f simple-test.yaml
```

This will:
- Poll JSONPlaceholder API every 30 seconds
- Create ConfigMaps for each user
- Use the public JSONPlaceholder API (no auth required)

## Complete Examples

The main sample file `konnektr_v1alpha1_httpqueryresource.yaml` contains multiple examples:

### 1. Basic Example (jsonplaceholder-users)
- Fetches users from JSONPlaceholder
- Creates ConfigMaps with user data
- No authentication required

### 2. Full-Featured Example (api-with-auth-and-status)
- Demonstrates bearer token authentication
- Creates Deployments based on API data
- Includes status update callbacks
- Shows finalizer usage

### 3. Advanced Example (nested-api-example)
- Shows complex JSONPath usage for nested responses
- Creates Services instead of ConfigMaps
- Demonstrates annotations and labels

### 4. API Key Example (apikey-auth-example)
- Shows API key authentication
- Demonstrates custom header names
- Includes status callbacks

## Authentication Examples

### Bearer Token
```yaml
authenticationRef:
  name: api-credentials
  type: bearer
  tokenKey: token
```

### API Key
```yaml
authenticationRef:
  name: apikey-credentials
  type: apikey
  apikeyKey: apikey
  apikeyHeader: "X-API-Key"
```

### Basic Auth
```yaml
authenticationRef:
  name: basic-credentials
  type: basic
  usernameKey: username
  passwordKey: password
```

## Status Updates

Status updates allow you to send HTTP requests when resources are created/updated:

```yaml
statusUpdate:
  url: "https://your-api.com/webhook"
  method: POST
  headers:
    Content-Type: "application/json"
  bodyTemplate: |
    {
      "resource": "{{ .Resource.metadata.name }}",
      "status": "{{ .Resource.status.phase }}",
      "timestamp": "{{ now | date \"2006-01-02T15:04:05Z07:00\" }}"
    }
```

## Testing with httpbin.org

The examples use httpbin.org for status updates, which is perfect for testing as it echoes back the requests you send.

## Deployment

1. Apply the CRDs:
   ```bash
   kubectl apply -f path/to/crds.yaml
   ```

2. Deploy the operator:
   ```bash
   kubectl apply -f path/to/operator-deployment.yaml
   ```

3. Apply a sample configuration:
   ```bash
   kubectl apply -f simple-test.yaml
   ```

4. Check the results:
   ```bash
   kubectl get httpqueryresources
   kubectl get configmaps -l app.kubernetes.io/managed-by=http-query-operator
   ```

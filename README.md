# HTTP Query Operator

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Build Status](https://github.com/konnektr-io/http-query-operator/actions/workflows/build-push.yaml/badge.svg)](https://github.com/konnektr-io/http-query-operator/actions/workflows/build-push.yaml)

## Overview

The HTTP Query Operator is a Kubernetes operator designed to manage Kubernetes resources based on the results of HTTP API calls. It periodically polls specified HTTP endpoints, processes the JSON responses, and uses Go templates to render Kubernetes manifests for each item returned by the API.

The operator handles the reconciliation loop, ensuring that the resources in the cluster match the desired state defined by the HTTP API responses and the template. This allows for dynamic configuration and resource management driven directly by external APIs and services.

## Features

* **CRD Driven:** Configuration is managed via an `HTTPQueryResource` Custom Resource Definition.
* **HTTP API Polling:** Periodically queries HTTP/HTTPS endpoints at a configurable interval.
* **Multiple Authentication:** Supports Basic Auth, Bearer Token, API Key, and OAuth2 Client Credentials authentication.
* **JSONPath Support:** Extract specific data from JSON responses using JSONPath expressions.
* **Go Templating:** Define Kubernetes resource manifests using Go templates with Sprig functions.
* **Item-to-Resource Mapping:** Each item in the API response typically generates one Kubernetes resource.
* **Status Updates:** Optionally send HTTP callbacks with resource status after reconciliation.
* **Secret Management:** Securely fetches authentication credentials from Kubernetes Secrets.
* **Reconciliation:** Creates, updates, and (optionally) deletes Kubernetes resources to match the API results.
* **Pruning:** Automatically cleans up resources previously created by the operator if they no longer correspond to an item in the API response (configurable).
* **Ownership:** Sets Owner References on created resources for automatic garbage collection by Kubernetes when the `HTTPQueryResource` is deleted.
* **Labeling:** Labels created resources for easy identification and potential pruning.

## Prerequisites

* **kubectl:** For interacting with the Kubernetes cluster.
* **Helm:** For installing the operator.
* **Kubernetes Cluster:** Access to a Kubernetes cluster (e.g., kind, Minikube, EKS, GKE, AKS).
* **HTTP API Endpoint:** A running HTTP/HTTPS API endpoint accessible from the Kubernetes cluster.

## Getting Started

### 1. Install the Operator using Helm

You can deploy the operator using Helm from the official chart repository:

```bash
helm repo add konnektr https://charts.konnektr.io
helm repo update
helm install http-query-operator konnektr/http-query-operator \
  --namespace <namespace> \
  --create-namespace \
  --set image.tag=<version> \
  --set gvkPattern="v1/ConfigMap;apps/v1/Deployment" \
  --set installCRDs=true
```

* By default, the image tag will match the Helm chart's `appVersion`.
* You can override any value in `values.yaml` using `--set` or a custom `values.yaml`.
* The `gvkPattern` parameter allows you to specify which Kubernetes resources the operator should manage.
* The CRDs are not installed by default; install with the installCRDs parameter or manually as described below.

#### Install the CRDs (required)

```bash
kubectl apply -f https://github.com/konnektr-io/http-query-operator/releases/latest/download/crds.yaml
```

#### Uninstall

```bash
helm uninstall http-query-operator -n <namespace>
```

### 2. Prepare the HTTP API

Ensure your HTTP API endpoint is running and accessible from your cluster. The API should return JSON responses that can be processed by the operator.

Example API response format:

```json
{
  "users": [
    {
      "user_id": 1,
      "username": "alice",
      "email": "alice@example.com",
      "status": "active"
    },
    {
      "user_id": 2,
      "username": "bob", 
      "email": "bob@example.com",
      "status": "active"
    }
  ]
}
```

### 3. Verify the Operator Pod

Check that the operator pod is running:

```bash
kubectl get pods -n <namespace>
# Look for a pod named like controller-manager-...

# View logs
kubectl logs -n <namespace> -l control-plane=controller-manager -f
```

### 4. Create Authentication Credentials Secret (Optional)

If your HTTP API requires authentication, create a Kubernetes Secret containing the authentication details. The operator will read credentials from this Secret.

**Example `api-credentials.yaml`:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: api-credentials
  # IMPORTANT: Deploy this secret in the same namespace as your HTTPQueryResource CR,
  # or specify the secret's namespace in the CR spec.
  namespace: default
type: Opaque
stringData:
  # For Basic Authentication
  username: "your_api_user"
  password: "your_api_password"
  
  # For Bearer Token Authentication
  token: "your_bearer_token"
  
  # For API Key Authentication
  apikey: "your_api_key"
  
  # For OAuth2 Client Credentials Authentication
  clientId: "your_oauth2_client_id"
  clientSecret: "your_oauth2_client_secret"
```

Apply the secret:

```bash
kubectl apply -f api-credentials.yaml
```

## Usage

Create an `HTTPQueryResource` custom resource to tell the operator which HTTP endpoint to query and how to generate resources.

**Example `config/samples/http_v1alpha1_httpqueryresource.yaml`:**

```yaml
apiVersion: konnektr.io/v1alpha1
kind: HTTPQueryResource
metadata:
  name: user-configmaps-example
  namespace: default # Namespace where this CR is deployed and where resources will be created by default
spec:
  # How often to query the HTTP endpoint and reconcile
  pollInterval: "1m"
  # Whether to delete resources if their corresponding API item disappears (default: true)
  prune: true
  http:
    # The HTTP endpoint URL
    url: "https://api.example.com/users"
    # HTTP method (default: GET)
    method: "GET"
    # Optional headers
    headers:
      Accept: "application/json"
      User-Agent: "http-query-operator"
    # Optional: JSONPath to extract array data from response
    responsePath: "$.users"
    # Optional: Authentication
    authenticationRef:
      name: api-credentials
      type: basic # or "bearer" or "apikey"
      # Optional: Custom key names in the secret
      # usernameKey: "username"
      # passwordKey: "password"
      # tokenKey: "token"
      # apikeyKey: "apikey"
      # apikeyHeader: "X-API-Key"
  # Go template for the Kubernetes resource(s)
  template: |
    apiVersion: v1
    kind: ConfigMap
    metadata:
      # Name must be unique per item. Use data from the API response.
      # Ensure the resulting name is DNS-compatible!
      name: user-{{ .Item.username | lower }}-config
      namespace: default
      labels:
        # Use API data in labels/annotations
        user_id: "{{ .Item.user_id }}"
        # This label is automatically added by the controller:
        # konnektr.io/managed-by: http-query-operator-controller
    data:
      email: "{{ .Item.email }}"
      status: "{{ .Item.status }}"
      username: "{{ .Item.username }}"
      # Example using Go template functions (time)
      managedTimestamp: "{{ now | date "2006-01-02T15:04:05Z07:00" }}"
  # Optional: HTTP callback for status updates
  statusUpdate:
    url: "https://api.example.com/users/{{ .Item.user_id }}/status"
    method: "PATCH"
    headers:
      Content-Type: "application/json"
    bodyTemplate: |
      {
        "kubernetes_status": "{{ .Resource.status | toJson }}",
        "updated_at": "{{ now | date "2006-01-02T15:04:05Z07:00" }}"
      }
    authenticationRef:
      name: api-credentials
      type: bearer
```

**Apply the sample CR:**

```bash
kubectl apply -f config/samples/http_v1alpha1_httpqueryresource.yaml -n default
```

**Check the results:**
After the `pollInterval` duration, the operator should query the HTTP endpoint and create resources based on the template.

```bash
# Check the status of the HTTPQueryResource
kubectl get httpqueryresource user-configmaps-example -n default -o yaml

# Check for created resources (ConfigMaps in this example)
kubectl get configmaps -n default -l konnektr.io/managed-by=http-query-operator-controller
kubectl get configmap user-alice-config -n default -o yaml # Example for user 'alice'
```

### Example with Deployment Management

Here is an example `HTTPQueryResource` Custom Resource that creates Kubernetes `Deployments` based on API data and sends status updates back to the API:

```yaml
apiVersion: konnektr.io/v1alpha1
kind: HTTPQueryResource
metadata:
  name: deployment-example
  namespace: default
spec:
  pollInterval: "2m"
  prune: true
  http:
    url: "https://api.example.com/applications"
    method: "GET"
    headers:
      Authorization: "Bearer ${TOKEN}"
    responsePath: "$.applications"
    authenticationRef:
      name: api-credentials
      type: bearer
  template: |
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: {{ .Item.name }}
      namespace: default
      labels:
        app_id: "{{ .Item.id }}"
    spec:
      replicas: {{ .Item.replicas | default 1 }}
      selector:
        matchLabels:
          app: {{ .Item.name }}
      template:
        metadata:
          labels:
            app: {{ .Item.name }}
        spec:
          containers:
          - name: {{ .Item.name }}
            image: {{ .Item.image }}
            ports:
            - containerPort: {{ .Item.port | default 8080 }}
  statusUpdate:
    url: "https://api.example.com/applications/{{ .Item.id }}/status"
    method: "PUT"
    headers:
      Content-Type: "application/json"
    bodyTemplate: |
      {
        "deployment_status": {
          "replicas": {{ .Resource.status.replicas | default 0 }},
          "available_replicas": {{ .Resource.status.availableReplicas | default 0 }},
          "ready_replicas": {{ .Resource.status.readyReplicas | default 0 }}
        },
        "last_updated": "{{ now | date "2006-01-02T15:04:05Z07:00" }}"
      }
    authenticationRef:
      name: api-credentials
      type: bearer
```

In this example:

* The HTTP request fetches application data from an API endpoint.
* The `template` generates a Kubernetes `Deployment` for each application.
* The `statusUpdate` sends deployment status back to the API after reconciliation.

### Example with OAuth2 Authentication

This example shows how to use OAuth2 Client Credentials flow to authenticate with your API:

```yaml
apiVersion: konnektr.io/v1alpha1
kind: HTTPQueryResource
metadata:
  name: oauth2-api-example
  namespace: default
spec:
  pollInterval: "5m"
  prune: true
  http:
    url: "https://api.oauth2example.com/v1/resources"
    method: "GET"
    headers:
      Accept: "application/json"
    responsePath: "$.data"  # Extract from {"data": [...]} response
    authenticationRef:
      name: oauth2-credentials
      type: oauth2
      tokenUrl: "https://auth.oauth2example.com/oauth2/token"
      scopes: "read:resources write:status"
  template: |
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: resource-{{ .Item.id }}
      namespace: default
    data:
      id: "{{ .Item.id }}"
      name: "{{ .Item.name }}"
      description: "{{ .Item.description }}"
      updated_at: "{{ now | date "2006-01-02T15:04:05Z07:00" }}"
```

The corresponding OAuth2 credentials secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: oauth2-credentials
  namespace: default
type: Opaque
stringData:
  clientId: "your_oauth2_client_id"
  clientSecret: "your_oauth2_client_secret"
```

In this example:

* The operator uses OAuth2 Client Credentials flow to get an access token from `tokenUrl`.
* The access token is automatically added to requests as `Authorization: Bearer <token>`.
* Tokens are automatically refreshed when they expire.
* Multiple scopes can be requested by separating them with spaces.

## CRD Specification (`HTTPQueryResourceSpec`)

* `pollInterval` (string, required): Duration string specifying how often to poll the HTTP endpoint (e.g., `"30s"`, `"5m"`, `"1h"`).
* `prune` (boolean, optional, default: `true`): If `true`, resources previously managed by this CR that no longer correspond to an item in the latest API response will be deleted.
* `http` (object, required):
  * `url` (string, required): The HTTP/HTTPS endpoint URL to query.
  * `method` (string, optional, default: `"GET"`): HTTP method (GET, POST, PUT, PATCH, DELETE).
  * `headers` (map, optional): HTTP headers to include in the request.
  * `body` (string, optional): Request body for POST/PUT/PATCH requests. Can be a Go template.
  * `responsePath` (string, optional, default: `"$"`): JSONPath expression to extract array data from response.
  * `authenticationRef` (object, optional): Reference to authentication configuration.
    * `name` (string, required): Name of the Secret containing authentication details.
    * `namespace` (string, optional): Namespace of the Secret. Defaults to the `HTTPQueryResource`'s namespace.
    * `type` (string, required, enum: `"basic"`, `"bearer"`, `"apikey"`, `"oauth2"`): Type of authentication.
    * `usernameKey` (string, optional): Key in the Secret for the username (basic auth). Defaults to `"username"`.
    * `passwordKey` (string, optional): Key in the Secret for the password (basic auth). Defaults to `"password"`.
    * `tokenKey` (string, optional): Key in the Secret for the token (bearer auth). Defaults to `"token"`.
    * `apikeyKey` (string, optional): Key in the Secret for the API key. Defaults to `"apikey"`.
    * `apikeyHeader` (string, optional): Header name for API key authentication. Defaults to `"X-API-Key"`.
    * `clientIdKey` (string, optional): Key in the Secret for OAuth2 client ID. Defaults to `"clientId"`.
    * `clientSecretKey` (string, optional): Key in the Secret for OAuth2 client secret. Defaults to `"clientSecret"`.
    * `tokenUrl` (string, optional): OAuth2 token endpoint URL for client credentials flow. Required for `oauth2` type.
    * `scopes` (string, optional): OAuth2 scopes to request (space-separated). Optional for `oauth2` type.
* `template` (string, required): A Go template string that renders a valid Kubernetes resource manifest (YAML or JSON).
  * **Template Context:** The template receives a map with the following structure:

  ```go
  {
      "Item": {
          "field1": value1,
          "field2": value2,
          // ... other fields from the API response item
      },
      "Index": 0 // Index of the item in the response array
  }
  ```
* `statusUpdate` (object, optional): Configuration for HTTP status update callbacks.
  * `url` (string, required): The HTTP/HTTPS endpoint URL for status updates. Can be a Go template.
  * `method` (string, optional, default: `"PATCH"`): HTTP method for status updates.
  * `headers` (map, optional): HTTP headers to include in the status update request.
  * `bodyTemplate` (string, required): Go template for the request body. Receives the resource data.
  * `authenticationRef` (object, optional): Authentication details for status updates (same structure as above).

  * **Template Context:** The template receives a map with the following structure for status updates:

  ```go
  {
      "Resource": { // The individual child resource being updated
          "apiVersion": "apps/v1",
          "kind": "Deployment",
          "metadata": {
              "name": "app-example",
              "namespace": "default",
              // ... other metadata fields
          },
          "status": {
              "replicas": 1,
              "availableReplicas": 1,
              // ... other status fields
          }
          // ... other resource fields
      },
      "Item": { // The original API response item that generated this resource
          "id": "123",
          "name": "app-example",
          // ... other API fields
      }
  }
  ```

  * You can use standard Go template functions and Sprig functions. Access item data via `.Item.field_name` and resource data via `.Resource.status.field_name`.

## Cascading Deletion and Finalizer Logic

By default, deleting an `HTTPQueryResource` will **not** delete the resources it manages (such as ConfigMaps, Deployments, etc).

If you want the operator to delete all managed resources when the `HTTPQueryResource` is deleted, you must explicitly add the following finalizer to the resource:

```yaml
metadata:
  finalizers:
    - konnektr.io/httpqueryresource-finalizer
```

When this finalizer is present, the operator will:

1. On deletion (when you run `kubectl delete httpqueryresource ...`), the operator will first delete all managed resources (those labeled with `konnektr.io/managed-by: <controller-name>`).
2. Once all managed resources are deleted, the operator will remove the finalizer, allowing the `HTTPQueryResource` to be deleted.

**How to use:**
- To enable cascading deletion, patch your resource before deleting:

  ```bash
  kubectl patch httpqueryresource <name> -n <namespace> --type='json' -p='[{"op": "add", "path": "/metadata/finalizers/-", "value": "konnektr.io/httpqueryresource-finalizer"}]'
  ```
- Then delete as usual:

  ```bash
  kubectl delete httpqueryresource <name> -n <namespace>
  ```

If the finalizer is not present, deleting the `HTTPQueryResource` will **not** delete any managed resources.

## Development

1. **Prerequisites:** Ensure Go, Docker, `kubectl`, `controller-gen`, and access to a Kubernetes cluster are set up.
2. **Clone:** `git clone <repository-url>`
3. **Modify Code:** Make changes to the API (`api/v1alpha1/`) or controller (`internal/controller/`).
4. **Regenerate Code:** After modifying API types or RBAC/CRD markers, run:

    ```bash
    # Regenerate deepcopy methods for API types
    controller-gen object paths=./api/v1alpha1

    # Regenerate CRD and RBAC manifests
    # Adjust paths if needed, especially on Windows: paths=./api/v1alpha1,./internal/controller
    controller-gen rbac:roleName=manager-role crd webhook paths=./api/v1alpha1,./internal/controller output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac
    ```

5. **Build:**

    ```bash
    go build ./...
    # Or build the container image (see step 4 in Getting Started)
    ```

6. **Deploy:** Re-deploy the operator using the steps in "Getting Started".

## Contributing

Contributions are welcome! Please follow standard GitHub practices: fork the repository, create a feature branch, make your changes, and submit a pull request. Ensure your code builds, passes any tests, and includes updates to documentation if necessary.

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

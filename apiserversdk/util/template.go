package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/mitchellh/mapstructure"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	api "github.com/ray-project/kuberay/proto/go_client"
)

// compute_template_middleware.go
func NewComputeTemplateMiddleware(_ kubernetes.Interface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			klog.Infoln("Get in to compute template middleware")

			namespace := r.PathValue("namespace")

			// Read request body
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Failed to read request body", http.StatusBadRequest)
				return
			}
			defer r.Body.Close()

			// Convert request body to Golang Map object
			requestMap, err := convertRequestBodyToMap(bodyBytes)
			if err != nil {
				http.Error(w, "Failed to convert request body to Golang map object", http.StatusBadRequest)
				return
			}
			klog.Infoln("Request Map: ", requestMap)

			// Convert Map to ClusterSpec
			clusterSpec, err := extractClusterSpec(requestMap)
			klog.Infof("Cluster Spec extracted: %v\n", clusterSpec)
			if err != nil {
				klog.Errorf("Failed to convert request body to ClusterSpec: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Create resource manager to use PopulateComputeTemplate
			clientManager := manager.NewClientManager()
			resourceManager := manager.NewResourceManager(&clientManager)

			// Use PopulateComputeTemplate to extract and fetch compute templates
			computeTemplateMap, err := resourceManager.PopulateComputeTemplate(context.Background(), clusterSpec, namespace)
			if err != nil {
				klog.Errorf("Failed to populate compute templates: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			klog.Infoln("Compute Templates: ", computeTemplateMap)

			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			next.ServeHTTP(w, r)
		})
	}
}

// Convert the request body to map
func convertRequestBodyToMap(requestBody []byte) (map[string]any, error) {
	var requestMap map[string]interface{}
	if err := yaml.Unmarshal(requestBody, &requestMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Kubernetes object: %w", err)
	}

	return requestMap, nil
}

// Convert request map into api.ClusterSpec
func extractClusterSpec(requestMap map[string]any) (*api.ClusterSpec, error) {
	// Extract the spec section
	specData, ok := requestMap["spec"]
	if !ok {
		return nil, fmt.Errorf("no spec found in request body")
	}

	// Convert specData to ClusterSpec
	var clusterSpec api.ClusterSpec
	if err := mapstructure.Decode(specData, &clusterSpec); err != nil {
		return nil, fmt.Errorf("failed to decode spec into ClusterSpec: %w", err)
	}

	klog.Infof("Extracted ClusterSpec with HeadGroup ComputeTemplate: %s", clusterSpec.HeadGroupSpec.GetComputeTemplate())

	return &clusterSpec, nil
}

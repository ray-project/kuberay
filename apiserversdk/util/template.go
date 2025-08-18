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

			// Convert request body to api.ClusterSpec
			clusterSpec, err := convertRequestBodyToClusterSpec(bodyBytes)
			klog.Infoln("Cluster Spec extracted: ", clusterSpec)
			if err != nil {
				klog.Errorf("Failed to convert request body to ClusterSpec: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Create resource manager to use PopulateComputeTemplate
			clientManager := manager.NewClientManager()
			resourceManager := manager.NewResourceManager(&clientManager)

			// Use PopulateComputeTemplate to extract and fetch compute templates
			computeTemplates, err := resourceManager.PopulateComputeTemplate(context.Background(), clusterSpec, namespace)
			if err != nil {
				klog.Errorf("Failed to populate compute templates: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// TODO: modify the resources based on the ClusterTemplate

			klog.Infof("Successfully extracted and fetched compute templates: %v", computeTemplates)

			// Restore request body and continue
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			next.ServeHTTP(w, r)
		})
	}
}

// convertRequestBodyToClusterSpec converts YAML/JSON request body to api.ClusterSpec
func convertRequestBodyToClusterSpec(requestBody []byte) (*api.ClusterSpec, error) {
	// Extract the spec section
	var kubeObj map[string]interface{}
	if err := yaml.Unmarshal(requestBody, &kubeObj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Kubernetes object: %w", err)
	}
	specData, ok := kubeObj["spec"]
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

package util

import (
	"bytes"
	"io"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
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

			// Process ComputeTemplate for RayCluster/RayJob/RayService
			// All three resources have the same ClusterSpec structure
			templates, err := ExtractComputeTemplateReferences(bodyBytes, namespace)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// TODO: Fetch and apply compute templates to the request body
			// For now, just continue with the original request
			_ = templates

			// Restore request body and continue
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			next.ServeHTTP(w, r)
		})
	}
}

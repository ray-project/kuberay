package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

// compute_template_middleware.go
func NewComputeTemplateMiddleware(_ kubernetes.Interface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			spec, ok := requestMap["spec"].(map[string]interface{})
			if !ok {
				klog.Errorf("spec is not a map")
				return
			}

			clientManager := manager.NewClientManager()
			resourceManager := manager.NewResourceManager(&clientManager)

			// TODO: This is the format for RayCluster, need to modify to apply also RayJob and RayService
			// For RayCluster, headGroupMap is directly under spec
			headGroupMap, ok := spec["headGroupSpec"].(map[string]interface{})
			if ok {
				computeTemplate, err := getComputeTemplate(context.Background(), resourceManager, headGroupMap, namespace)
				if err != nil {
					klog.Errorf("Failed to get compute template for head group: %v", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if computeTemplate != nil {
					applyComputeTemplateToRequest(computeTemplate, &headGroupMap, "head")
					klog.Infof("Applied compute template '%s' to headGroupSpecs", computeTemplate.GetName())
				}
			}

			// Apply compute templates to worker groups
			workerGroupSpecs, ok := spec["workerGroupSpecs"].([]interface{})
			if ok {
				for i, workerGroupSpec := range workerGroupSpecs {
					if workerGroupMap, ok := workerGroupSpec.(map[string]interface{}); ok {
						computeTemplate, err := getComputeTemplate(context.Background(), resourceManager, workerGroupMap, namespace)
						if err != nil {
							klog.Errorf("Failed to get compute template for worker group: %v", err)
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						if computeTemplate != nil {
							applyComputeTemplateToRequest(computeTemplate, &workerGroupMap, "worker")
							klog.Infof("Applied compute template '%s' to workerGroupSpecs[%d]", computeTemplate.GetName(), i)
						}
					}
				}
			}

			// Convert the modified requestMap back to bytes
			modifiedBodyBytes, err := yaml.Marshal(requestMap)
			if err != nil {
				klog.Errorf("Failed to marshal modified request map: %v", err)
				http.Error(w, "Failed to process request", http.StatusInternalServerError)
				return
			}

			// Update Content-Length header to match the new body size
			r.ContentLength = int64(len(modifiedBodyBytes))
			r.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBodyBytes)))
			r.Body = io.NopCloser(bytes.NewReader(modifiedBodyBytes))
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

// Get the compute template by extracting the name from request and query the compute template
func getComputeTemplate(ctx context.Context, resourceManager *manager.ResourceManager, clusterSpecMap map[string]interface{}, nameSpace string) (*api.ComputeTemplate, error) {
	name, ok := clusterSpecMap["computeTemplate"].(string)
	if !ok {
		// No compute template name found, directly return
		return nil, nil
	}

	configMap, err := resourceManager.GetComputeTemplate(ctx, name, nameSpace)
	if err != nil {
		return nil, fmt.Errorf("Cannot get compute template for name '%s' in namespace '%s', error: %w", name, nameSpace, err)
	}
	computeTemplate := model.FromKubeToAPIComputeTemplate(configMap)

	return computeTemplate, nil
}

// Apply the computeTemplate into the clusterSpec map. The clusterSpec map is the map representation
// for headGroupSpec or workerGroupSpec
func applyComputeTemplateToRequest(computeTemplate *api.ComputeTemplate, clusterSpecMap *map[string]interface{}, group string) {
	// calculate resources
	cpu := fmt.Sprint(computeTemplate.GetCpu())
	memory := fmt.Sprintf("%d%s", computeTemplate.GetMemory(), "Gi")

	if template, ok := (*clusterSpecMap)["template"].(map[string]interface{}); ok {
		// Add compute template name to annotation

		metadata, ok := template["metadata"].(map[string]interface{})
		if !ok {
			metadata = make(map[string]interface{})
			template["metadata"] = metadata
		}
		annotations, ok := metadata["annotations"].(map[string]interface{})
		if !ok {
			annotations = make(map[string]interface{})
			metadata["annotations"] = annotations
		}
		annotations[util.RayClusterComputeTemplateAnnotationKey] = computeTemplate.Name

		// apply resources to containers
		if spec, ok := template["spec"].(map[string]interface{}); ok {
			if containers, ok := spec["containers"].([]interface{}); ok {
				for _, container := range containers {
					if containerMap, ok := container.(map[string]interface{}); ok {
						// Get or create resources section for this container
						resources, exists := containerMap["resources"].(map[string]interface{})
						if !exists {
							resources = make(map[string]interface{})
							containerMap["resources"] = resources
						}

						// Set limits
						limits, exists := resources["limits"].(map[string]interface{})
						if !exists {
							limits = make(map[string]interface{})
							resources["limits"] = limits
						}
						limits["cpu"] = cpu
						limits["memory"] = memory

						// Set requests
						requests, exists := resources["requests"].(map[string]interface{})
						if !exists {
							requests = make(map[string]interface{})
							resources["requests"] = requests
						}
						requests["cpu"] = cpu
						requests["memory"] = memory

						// Only apply followings if container name is "ray-head" for head group or "ray-worker"
						// for worker group
						if containerMap["name"] == fmt.Sprintf("ray-%s", group) {
							if gpu := computeTemplate.GetGpu(); gpu != 0 {
								accelerator := "nvidia.com/gpu"
								if len(computeTemplate.GetGpuAccelerator()) != 0 {
									accelerator = computeTemplate.GetGpuAccelerator()
								}
								limits[accelerator] = gpu
								requests[accelerator] = gpu
							}

							for k, v := range computeTemplate.GetExtendedResources() {
								limits[k] = v
								requests[k] = v
							}

						}
					}
				}
			}

			if computeTemplate.Tolerations != nil {
				// Get existing tolerations
				var tolerations []interface{}
				if existingTolerations, exists := spec["tolerations"].([]interface{}); exists {
					tolerations = existingTolerations
				} else {
					tolerations = make([]interface{}, 0)
				}

				// Add new tolerations from compute template
				for _, t := range computeTemplate.Tolerations {
					toleration := map[string]interface{}{
						"key":      t.Key,
						"operator": t.Operator,
						"value":    t.Value,
						"effect":   t.Effect,
					}
					tolerations = append(tolerations, toleration)
				}

				spec["tolerations"] = tolerations
			}
		}
	}
}

package util

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

// compute_template_middleware.go
func NewComputeTemplateMiddleware(clientManager manager.ClientManagerInterface) func(http.Handler) http.Handler {
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
			contentType := r.Header.Get("Content-Type")
			requestMap, err := convertRequestBodyToMap(bodyBytes, contentType)
			if err != nil {
				klog.Errorf("Failed to convert request body to map: %v", err)
				http.Error(w, "Failed to convert request body to Golang map object", http.StatusBadRequest)
				return
			}
			spec, ok := requestMap["spec"].(map[string]interface{})
			if !ok {
				klog.Infof("ComputeTemplate middleware: spec is not a map, skipping compute template processing")
				// Continue with original request body without compute template processing
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				next.ServeHTTP(w, r)
				return
			}

			// Process compute templates and apply them to the request
			var headGroupMap map[string]interface{}
			var workerGroupMaps []interface{}
			if rayClusterSpec, ok := spec["rayClusterSpec"].(map[string]interface{}); ok {
				// For RayJob, get from spec.rayClusterSpec.headGroupSpec and spec.rayClusterSpec.workerGroupSpecs
				if headGroup, ok := rayClusterSpec["headGroupSpec"].(map[string]interface{}); ok {
					headGroupMap = headGroup
				}
				if workerGroups, ok := rayClusterSpec["workerGroupSpecs"].([]interface{}); ok {
					workerGroupMaps = workerGroups
				}
			} else if rayClusterConfig, ok := spec["rayClusterConfig"].(map[string]interface{}); ok {
				// For RayService, get from spec.rayClusterConfig.headGroupSpec and spec.rayClusterConfig.workerGroupSpecs
				if headGroup, ok := rayClusterConfig["headGroupSpec"].(map[string]interface{}); ok {
					headGroupMap = headGroup
				}
				if workerGroups, ok := rayClusterConfig["workerGroupSpecs"].([]interface{}); ok {
					workerGroupMaps = workerGroups
				}
			} else {
				// For RayCluster, get from spec.headGroupSpec and spec.workerGroupSpecs
				if headGroup, ok := spec["headGroupSpec"].(map[string]interface{}); ok {
					headGroupMap = headGroup
				}
				if workerGroups, ok := spec["workerGroupSpecs"].([]interface{}); ok {
					workerGroupMaps = workerGroups
				}
			}

			resourceManager := manager.NewResourceManager(clientManager)

			if headGroupMap != nil {
				computeTemplate, err := getComputeTemplate(context.Background(), resourceManager, headGroupMap, namespace)
				if err != nil {
					klog.Errorf("ComputeTemplate middleware: Failed to get compute template for head group: %v", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if computeTemplate != nil {
					applyComputeTemplateToRequest(computeTemplate, &headGroupMap, "head")
				}
			}

			// Apply compute templates to worker groups
			for i, workerGroupSpec := range workerGroupMaps {
				if workerGroupMap, ok := workerGroupSpec.(map[string]interface{}); ok {
					computeTemplate, err := getComputeTemplate(context.Background(), resourceManager, workerGroupMap, namespace)
					if err != nil {
						klog.Errorf("ComputeTemplate middleware: Failed to get compute template for worker group %d: %v", i, err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if computeTemplate != nil {
						klog.Infof("ComputeTemplate middleware: Applying compute template %s to worker group %d", computeTemplate.Name, i)
						applyComputeTemplateToRequest(computeTemplate, &workerGroupMap, "worker")
					}
				}
			}

			// Convert the modified requestMap to JSON since K8s API expects JSON format
			jsonBytes, err := convertMapToJSON(requestMap)
			if err != nil {
				klog.Errorf("ComputeTemplate middleware: Failed to convert to JSON: %v", err)
				http.Error(w, "Failed to process request", http.StatusInternalServerError)
				return
			}

			klog.Infof("ComputeTemplate middleware: Successfully processed request, sending to next handler")
			// Update Content-Type to application/json and Content-Length header to match the new body size
			r.Header.Set("Content-Type", "application/json")
			r.ContentLength = int64(len(jsonBytes))
			r.Header.Set("Content-Length", fmt.Sprintf("%d", len(jsonBytes)))
			r.Body = io.NopCloser(bytes.NewReader(jsonBytes))

			next.ServeHTTP(w, r)
		})
	}
}

// Convert the request body to map, handling both JSON and YAML formats
func convertRequestBodyToMap(requestBody []byte, contentType string) (map[string]any, error) {
	var requestMap map[string]interface{}

	// Check content type to determine format
	if strings.Contains(contentType, "application/json") {
		if err := json.Unmarshal(requestBody, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
		}
	} else if strings.Contains(contentType, "application/yaml") {
		if err := yaml.Unmarshal(requestBody, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
		}
	} else {
		return nil, fmt.Errorf("Cannot unmarshal content type that's not JSON or YAML: %s", contentType)
	}

	return requestMap, nil
}

// Convert YAML request map to JSON bytes for K8s API
func convertMapToJSON(requestMap map[string]interface{}) ([]byte, error) {
	jsonBytes, err := json.Marshal(requestMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request map to JSON: %w", err)
	}
	return jsonBytes, nil
}

// Get the compute template by extracting the name from request and query the compute template
func getComputeTemplate(ctx context.Context, resourceManager *manager.ResourceManager, clusterSpecMap map[string]interface{}, nameSpace string) (*api.ComputeTemplate, error) {
	name, ok := clusterSpecMap["computeTemplate"].(string)
	if !ok {
		// No compute template name found, directly return
		klog.Infof("ComputeTemplate middleware: No computeTemplate field found in spec")
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

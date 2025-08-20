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
	"github.com/ray-project/kuberay/apiserver/pkg/util"
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

			// TODO: a function to inject the compute template to the container
			// 	- for head -> directly apply to the ray-head container
			//  - For worker -> use the function within the loop, apply to the ray-worker container

			// Type assert each level of the nested map
			spec, ok := requestMap["spec"].(map[string]interface{})
			if !ok {
				klog.Errorf("spec is not a map")
				return
			}

			// TODO: This is the format for RayCluster, need to modify to apply also RayJob and RayService
			// For RayCluster, headGroupSpec is directly under spec
			headGroupSpec, ok := spec["headGroupSpec"].(map[string]interface{})
			if !ok {
				klog.Errorf("headGroupSpec is not a map")
				return
			}
			computeTemplate := computeTemplateMap[clusterSpec.HeadGroupSpec.ComputeTemplate]
			applyComputeTemplateToRequest(computeTemplate, &headGroupSpec, "head")
			klog.Infoln("head group spec after injection: ", headGroupSpec)

			// Apply compute templates to worker groups
			workerGroupSpecs, ok := spec["workerGroupSpecs"].([]interface{})
			if !ok {
				klog.Errorf("Cannot convert workerGroupSpecs to a map")
				return
			}
			for i, workerSpec := range clusterSpec.WorkerGroupSpec {
				if i < len(workerGroupSpecs) {
					if workerGroupMap, ok := workerGroupSpecs[i].(map[string]interface{}); ok {
						computeTemplate = computeTemplateMap[workerSpec.ComputeTemplate]
						applyComputeTemplateToRequest(computeTemplate, &workerGroupMap, "worker")
						klog.Infof("Applied compute template to workerGroupSpecs[%d]", i)
					}
				}
			}
			klog.Infoln("worker group spec after injection: ", workerGroupSpecs)

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

// Apply the computeTemplate into the clusterSpec map. The clusterSpec map is the map representation
// for headGroupSpec or workerGroupSpec
func applyComputeTemplateToRequest(computeTemplate *api.ComputeTemplate, clusterSpecMap *map[string]interface{}, group string) {
	// put resources in the cmpute template into the containers' resource field
	// group: head or worker

	// 1. Add metadata.annotation: buildNodeGroupAnnotations(computeRuntime, spec.Image)
	// 	-> template.metadata.annotation

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
				for i, container := range containers {
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

						klog.Infof("Applied resources to container[%d] in %s group: CPU=%s, Memory=%s", i, group, cpu, memory)

						// Only apply followings if container name is "ray-head" for head group or "ray-worker"
						// for worker group
						if containerMap["name"] == fmt.Sprintf("ray-%s", group) {
							// TODO: keep working from here
							klog.Infoln("Parsing more resources")
						}
					}
				}
			}
		}
	}
}

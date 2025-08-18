package util

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"
)

// TemplateReference represents a compute template reference found in the request
type TemplateReference struct {
	Name      string // Template name
	Namespace string // Template namespace
	JSONPath  string // JSON path where template was found (e.g., "spec.headGroupSpec", "spec.rayClusterSpec.workerGroupSpecs[0]")
}

// ExtractComputeTemplateReferences extracts all compute template references from a request body
// This works for RayCluster, RayJob, and RayService requests based on their CRD structures
func ExtractComputeTemplateReferences(requestBody []byte, namespace string) ([]TemplateReference, error) {
	var request map[string]any
	if err := json.Unmarshal(requestBody, &request); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request body: %w", err)
	}

	var templates []TemplateReference

	// Check if we have a 'spec' field
	spec, ok := request["spec"].(map[string]any)
	if !ok {
		// No spec found, no templates to extract
		return templates, nil
	}

	if rayClusterSpec, exists := spec["rayClusterSpec"]; exists {
		// RayJob: spec.rayClusterSpec.headGroupSpec and spec.rayClusterSpec.workerGroupSpecs
		if clusterSpec, ok := rayClusterSpec.(map[string]any); ok {
			templates = append(templates, extractFromClusterSpec(clusterSpec, "spec.rayClusterSpec")...)
		}
	} else if rayClusterConfig, exists := spec["rayClusterConfig"]; exists {
		// RayService: spec.rayClusterConfig.headGroupSpec and spec.rayClusterConfig.workerGroupSpecs
		if clusterSpec, ok := rayClusterConfig.(map[string]any); ok {
			templates = append(templates, extractFromClusterSpec(clusterSpec, "spec.rayClusterConfig")...)
		}
	} else {
		// RayCluster: spec.headGroupSpec and spec.workerGroupSpecs
		templates = append(templates, extractFromClusterSpec(spec, "spec")...)
	}

	// Set namespace for all templates
	for i := range templates {
		templates[i].Namespace = namespace
	}

	klog.Infoln("Templates: ", templates)

	return templates, nil
}

// extractFromClusterSpec extracts compute templates from a ClusterSpec structure
func extractFromClusterSpec(clusterSpec map[string]any, basePath string) []TemplateReference {
	var templates []TemplateReference

	// Extract from headGroupSpec
	if headGroupSpec, ok := clusterSpec["headGroupSpec"].(map[string]any); ok {
		if templateName, ok := headGroupSpec["computeTemplate"].(string); ok && templateName != "" {
			templates = append(templates, TemplateReference{
				Name:     templateName,
				JSONPath: basePath + ".headGroupSpec",
			})
		}
	}

	// Extract from workerGroupSpecs array (note: "workerGroupSpecs" not "workerGroupSpec")
	if workerGroupSpecs, ok := clusterSpec["workerGroupSpecs"].([]any); ok {
		for i, wgs := range workerGroupSpecs {
			if workerGroup, ok := wgs.(map[string]any); ok {
				if templateName, ok := workerGroup["computeTemplate"].(string); ok && templateName != "" {
					templates = append(templates, TemplateReference{
						Name:     templateName,
						JSONPath: fmt.Sprintf("%s.workerGroupSpecs[%d]", basePath, i),
					})
				}
			}
		}
	}

	return templates
}

// HasComputeTemplate checks if the request body contains any compute template references
func HasComputeTemplate(requestBody []byte) bool {
	templates, err := ExtractComputeTemplateReferences(requestBody, "")
	if err != nil {
		return false
	}
	return len(templates) > 0
}

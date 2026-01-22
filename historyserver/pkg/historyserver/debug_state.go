package historyserver

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// NodeDebugState represents parsed data from a node's debug_state.txt file
type NodeDebugState struct {
	NodeID     string
	NodeName   string
	NodeGroup  string
	IsIdle     bool
	IsDraining bool
	Total      map[string]float64
	Available  map[string]float64
}

// ParseDebugState parses the content of a debug_state.txt file
// and extracts cluster resource scheduler state information.
func ParseDebugState(content string) (*NodeDebugState, error) {
	state := &NodeDebugState{
		Total:     make(map[string]float64),
		Available: make(map[string]float64),
	}

	lines := strings.Split(content, "\n")

	// Parse Node ID and Node name from the first few lines
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "Node ID:"); ok {
			state.NodeID = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "Node name:"); ok {
			state.NodeName = strings.TrimSpace(after)
		}
	}

	// Find the line containing cluster_resource_scheduler state
	for i, line := range lines {
		if strings.Contains(line, "cluster_resource_scheduler state:") {
			// The actual data is on the next line
			if i+1 < len(lines) {
				dataLine := lines[i+1]
				parseClusterResourceSchedulerLine(dataLine, state)
			}
			break
		}
	}

	return state, nil
}

// parseClusterResourceSchedulerLine parses the complex line containing resource info
// Example format:
// Local id: -123 Local resources: {"total":{memory: [100], ...}}, "available": {...}}, "labels":{...} is_draining: 0 is_idle: 1
func parseClusterResourceSchedulerLine(line string, state *NodeDebugState) {
	// Extract is_idle
	isIdleRegex := regexp.MustCompile(`is_idle:\s*(\d+)`)
	if match := isIdleRegex.FindStringSubmatch(line); len(match) > 1 {
		state.IsIdle = match[1] == "1"
	}

	// Extract is_draining
	isDrainingRegex := regexp.MustCompile(`is_draining:\s*(\d+)`)
	if match := isDrainingRegex.FindStringSubmatch(line); len(match) > 1 {
		state.IsDraining = match[1] == "1"
	}

	// Extract node group from labels
	// Format: "labels":{"ray.io/node-group":"headgroup",...}
	nodeGroupRegex := regexp.MustCompile(`"ray\.io/node-group"\s*:\s*"([^"]+)"`)
	if match := nodeGroupRegex.FindStringSubmatch(line); len(match) > 1 {
		state.NodeGroup = match[1]
	}

	// Extract total resources
	// Format: "total":{memory: [100000000000000], object_store_memory: [14396805120000], ...}
	totalRegex := regexp.MustCompile(`"total"\s*:\s*\{([^}]+)\}`)
	if match := totalRegex.FindStringSubmatch(line); len(match) > 1 {
		state.Total = parseResourceMap(match[1])
	}

	// Extract available resources
	availableRegex := regexp.MustCompile(`"available"\s*:\s*\{([^}]+)\}`)
	if match := availableRegex.FindStringSubmatch(line); len(match) > 1 {
		state.Available = parseResourceMap(match[1])
	}
}

// parseResourceMap parses a resource string like "memory: [100], CPU: [10]"
// Values are scaled by 10000 in Ray's internal format, so we divide accordingly
func parseResourceMap(resourceStr string) map[string]float64 {
	resources := make(map[string]float64)

	// Match patterns like "memory: [100000000000000]" or "CPU: [10000]"
	// Also handles "node:10.244.0.9: [10000]" and "object_store_memory: [14396805120000]"
	resourceRegex := regexp.MustCompile(`([a-zA-Z0-9_:./\-_]+):\s*\[([0-9.e+\-]+)\]`)
	matches := resourceRegex.FindAllStringSubmatch(resourceStr, -1)

	for _, match := range matches {
		if len(match) > 2 {
			key := strings.TrimSpace(match[1])
			valueStr := match[2]

			// Parse the value (handles scientific notation like 1e+10)
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				logrus.Debugf("Failed to parse resource value %s: %v", valueStr, err)
				continue
			}

			// Skip node-specific resources like "node:10.244.0.9"
			if strings.HasPrefix(key, "node:") {
				continue
			}

			// Ray scales resources by 10000 internally
			// Divide to get actual values
			resources[key] = value / 10000.0
		}
	}

	return resources
}

// GetUsedResources calculates used = total - available
func (s *NodeDebugState) GetUsedResources() map[string]float64 {
	used := make(map[string]float64)
	for key, total := range s.Total {
		available := s.Available[key]
		usedVal := total - available
		if usedVal < 0 {
			usedVal = 0
		}
		used[key] = usedVal
	}
	return used
}

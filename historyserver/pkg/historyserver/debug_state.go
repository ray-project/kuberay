package historyserver

import (
	"bufio"
	"io"
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

var (
	// Extracts the node group label from "ray.io/node-group":"<group name>"
	reNodeGroup = regexp.MustCompile(`"ray\.io/node-group"\s*:\s*"([^"]+)"`)
	// Matches the first "total": {...} on the line, which is the Local resources section
	reTotal = regexp.MustCompile(`"total"\s*:\s*\{([^}]+)\}`)
	// Matches the first "available": {...} on the line, which is the Local resources section
	reAvailable = regexp.MustCompile(`"available"\s*:\s*\{([^}]+)\}`)
	// Matches resource key-value pairs like "memory: [100000000000000]", "CPU: [10000]", or "object_store_memory: [14396805120000]"
	// Also matches node-specific resources like "node:10.244.0.9: [10000]"
	// and scientific notation values like [1e-3] or [1e+10].
	reResourceKV = regexp.MustCompile(`([a-zA-Z0-9_:./\-]+):\s*\[([0-9.e+\-]+)\]`)
)

const rayResourceScale = 10000.0

// ParseDebugState parses the content of a debug_state.txt file
// and extracts cluster resource scheduler state information.
// It rebuilds the Resources section exactly, and Idle section most of the time
func ParseDebugState(rd io.Reader) (*NodeDebugState, error) {
	state := &NodeDebugState{
		Total:     make(map[string]float64),
		Available: make(map[string]float64),
	}

	reader := bufio.NewReader(rd)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		// Parse Node ID and Node name from the first few lines
		if after, ok := strings.CutPrefix(line, "Node ID:"); ok {
			state.NodeID = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "Node name:"); ok {
			state.NodeName = strings.TrimSpace(after)
		} else if strings.Contains(line, "cluster_resource_scheduler state:") {
			// The actual data is on the next line
			nextLine, nextErr := reader.ReadString('\n')
			if nextErr != nil && nextErr != io.EOF {
				return nil, nextErr
			}
			parseClusterResourceSchedulerLine(nextLine, state)
			break
		}

		if err == io.EOF {
			break
		}
	}

	return state, nil
}

// parseClusterResourceSchedulerLine parses the complex line containing resource info
func parseClusterResourceSchedulerLine(line string, state *NodeDebugState) {
	// Extract is_idle
	state.IsIdle = strings.Contains(line, "is_idle: 1")

	// Extract is_draining
	state.IsDraining = strings.Contains(line, "is_draining: 1")

	// Extract node group name from "ray.io/node-group":"<group_name>"
	if m := reNodeGroup.FindStringSubmatch(line); len(m) > 1 {
		state.NodeGroup = m[1]
	}
	// Extract total resources from "total":{...} block
	if m := reTotal.FindStringSubmatch(line); len(m) > 1 {
		state.Total = parseResourceMap(m[1])
	}
	// Extract available resources from "available":{...} block
	if m := reAvailable.FindStringSubmatch(line); len(m) > 1 {
		state.Available = parseResourceMap(m[1])
	}
}

// parseResourceMap parses a resource string like "memory: [100], CPU: [10]"
// Values are scaled by 10000 in Ray's internal format, so we divide accordingly
func parseResourceMap(resourceStr string) map[string]float64 {
	resources := make(map[string]float64)

	matches := reResourceKV.FindAllStringSubmatch(resourceStr, -1)
	for _, match := range matches {
		if len(match) > 2 {
			key := strings.TrimSpace(match[1]) // e.g., "memory" or "object_store_memory"
			valueStr := match[2]               // e.g., "100000000000000"

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
			resources[key] = value / rayResourceScale
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

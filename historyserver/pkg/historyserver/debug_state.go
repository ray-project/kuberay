package historyserver

import (
	"bufio"
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
	reNodeGroup  = regexp.MustCompile(`"ray\.io/node-group"\s*:\s*"([^"]+)"`)
	reTotal      = regexp.MustCompile(`"total"\s*:\s*\{([^}]+)\}`)
	reAvailable  = regexp.MustCompile(`"available"\s*:\s*\{([^}]+)\}`)
	reResourceKV = regexp.MustCompile(`([a-zA-Z0-9_:./\-]+):\s*\[([0-9.e+\-]+)\]`)
)

const rayResourceScale = 10000.0

// ParseDebugState parses the content of a debug_state.txt file
// and extracts cluster resource scheduler state information.
// It rebuilds the Resources section exactly, and Idle section most of the time
func ParseDebugState(content string) (*NodeDebugState, error) {
	state := &NodeDebugState{
		Total:     make(map[string]float64),
		Available: make(map[string]float64),
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()

		// Parse Node ID and Node name from the first few lines
		if after, ok := strings.CutPrefix(line, "Node ID:"); ok {
			state.NodeID = strings.TrimSpace(after)
			continue
		}
		if after, ok := strings.CutPrefix(line, "Node name:"); ok {
			state.NodeName = strings.TrimSpace(after)
			continue
		}

		if strings.Contains(line, "cluster_resource_scheduler state:") {
			// The actual data is on the next line
			if scanner.Scan() {
				parseClusterResourceSchedulerLine(scanner.Text(), state)
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return state, nil
}

// parseClusterResourceSchedulerLine parses the complex line containing resource info
func parseClusterResourceSchedulerLine(line string, state *NodeDebugState) {
	// Extract is_idle
	state.IsIdle = strings.Contains(line, "is_idle: 1")

	// Extract is_draining
	state.IsDraining = strings.Contains(line, "is_draining: 1")

	// Extract node group from labels
	if m := reNodeGroup.FindStringSubmatch(line); len(m) > 1 {
		state.NodeGroup = m[1]
	}
	// Extract total resources
	if m := reTotal.FindStringSubmatch(line); len(m) > 1 {
		state.Total = parseResourceMap(m[1])
	}
	// Extract available resources
	if m := reAvailable.FindStringSubmatch(line); len(m) > 1 {
		state.Available = parseResourceMap(m[1])
	}
}

// parseResourceMap parses a resource string like "memory: [100], CPU: [10]"
// Values are scaled by 10000 in Ray's internal format, so we divide accordingly
func parseResourceMap(resourceStr string) map[string]float64 {
	resources := make(map[string]float64)

	// Match patterns like "memory: [100000000000000]" or "CPU: [10000]"
	// Also handles "node:10.244.0.9: [10000]" and "object_store_memory: [14396805120000]"
	matches := reResourceKV.FindAllStringSubmatch(resourceStr, -1)
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

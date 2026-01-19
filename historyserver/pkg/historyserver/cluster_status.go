package historyserver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// calculateUsedResources aggregates resources from running tasks and alive actors.
func CalculateUsedResources(tasks []eventtypes.Task, actors []eventtypes.Actor) map[string]float64 {
	used := make(map[string]float64)

	// Sum resources from running tasks.
	for _, task := range tasks {
		if task.State == eventtypes.RUNNING ||
			task.State == eventtypes.RUNNING_IN_RAY_GET ||
			task.State == eventtypes.RUNNING_IN_RAY_WAIT {
			for resource, amount := range task.RequiredResources {
				used[resource] += amount
			}
		}
	}

	// Sum resources from alive actors.
	for _, actor := range actors {
		if actor.State == eventtypes.ALIVE {
			for resource, amount := range actor.RequiredResources {
				used[resource] += amount
			}
		}
	}

	return used
}

// calculateNodeStats calculates node statistics from node list.
// Returns: activeNodes (map of nodeGroup -> count), failedNodes (list of node IDs), totalResources, latestTimestamp.
func CalculateNodeStats(nodes []eventtypes.Node) (map[string]int, []string, map[string]float64, time.Time) {
	activeNodes := make(map[string]int)
	failedNodes := []string{}
	totalResources := make(map[string]float64)
	var latestTimestamp time.Time

	for _, node := range nodes {
		// Track latest timestamp for status header.
		if node.StartTimestamp.After(latestTimestamp) {
			latestTimestamp = node.StartTimestamp
		}
		if node.DeadTimestamp.After(latestTimestamp) {
			latestTimestamp = node.DeadTimestamp
		}

		switch node.State {
		case eventtypes.NODE_ALIVE:
			nodeGroup := node.NodeGroup
			if nodeGroup == "" {
				nodeGroup = "default"
			}
			activeNodes[nodeGroup]++

			// Aggregate total resources from alive nodes.
			for resource, amount := range node.Resources {
				// Skip node-specific resources like "node:10.244.0.7".
				if strings.HasPrefix(resource, "node:") {
					continue
				}
				totalResources[resource] += amount
			}
		case eventtypes.NODE_DEAD:
			// Use NodeID if available, otherwise use IP.
			nodeIdentifier := node.NodeID
			if nodeIdentifier == "" {
				nodeIdentifier = node.NodeIPAddress
			}
			failedNodes = append(failedNodes, nodeIdentifier)
		}
	}

	return activeNodes, failedNodes, totalResources, latestTimestamp
}

// calculateAvailableResources calculates available = total - used.
func CalculateAvailableResources(total, used map[string]float64) map[string]float64 {
	available := make(map[string]float64)

	for resource, totalAmount := range total {
		usedAmount := used[resource]
		avail := totalAmount - usedAmount
		if avail < 0 {
			avail = 0
		}
		available[resource] = avail
	}

	return available
}

// calculates pending resource demands from tasks and actors.
func CalculateResourceDemands(tasks []eventtypes.Task, actors []eventtypes.Actor) []ResourceDemand {
	demandMap := make(map[string]*ResourceDemand)

	// Helper to create a key from resources map.
	resourceKey := func(resources map[string]float64) string {
		if len(resources) == 0 {
			return ""
		}
		var parts []string
		for k, v := range resources {
			parts = append(parts, fmt.Sprintf("%s:%s", k, strconv.FormatFloat(v, 'f', -1, 64)))
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	}

	// Count pending tasks.
	for _, task := range tasks {
		if isPendingTaskState(task.State) && len(task.RequiredResources) > 0 {
			key := resourceKey(task.RequiredResources)
			if key == "" {
				continue
			}
			if _, exists := demandMap[key]; !exists {
				demandMap[key] = &ResourceDemand{
					Resources: task.RequiredResources,
					Count:     0,
				}
			}
			demandMap[key].Count++
		}
	}

	// Count pending actors.
	for _, actor := range actors {
		if isPendingActorState(actor.State) && len(actor.RequiredResources) > 0 {
			key := resourceKey(actor.RequiredResources)
			if key == "" {
				continue
			}
			if _, exists := demandMap[key]; !exists {
				demandMap[key] = &ResourceDemand{
					Resources: actor.RequiredResources,
					Count:     0,
				}
			}
			demandMap[key].Count++
		}
	}

	// Convert to slice.
	keys := make([]string, 0, len(demandMap))
	for key := range demandMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var demands []ResourceDemand
	for _, key := range keys {
		demands = append(demands, *demandMap[key])
	}
	return demands
}

func isPendingTaskState(state eventtypes.TaskStatus) bool {
	switch state {
	case eventtypes.PENDING_ARGS_AVAIL, eventtypes.PENDING_NODE_ASSIGNMENT,
		eventtypes.PENDING_OBJ_STORE_MEM_AVAIL, eventtypes.PENDING_ARGS_FETCH,
		eventtypes.SUBMITTED_TO_WORKER, eventtypes.PENDING_ACTOR_TASK_ARGS_FETCH,
		eventtypes.PENDING_ACTOR_TASK_ORDERING_OR_CONCURRENCY:
		return true
	default:
		return false
	}
}

func isPendingActorState(state eventtypes.StateType) bool {
	switch state {
	case eventtypes.PENDING_CREATION, eventtypes.DEPENDENCIES_UNREADY:
		return true
	default:
		return false
	}
}

// formatAutoscalingStatus formats the autoscaling status string to match Ray dashboard format.
func FormatAutoscalingStatus(
	activeNodes map[string]int,
	failedNodes []string,
	usedResources map[string]float64,
	totalResources map[string]float64,
	demands []ResourceDemand,
	dataTimestamp time.Time,
) string {
	var sb strings.Builder

	// use data timestamp if available, otherwise current time.
	ts := dataTimestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	sb.WriteString(fmt.Sprintf("======== Autoscaler status: %s ========\n", ts.Format("2006-01-02 15:04:05.000000")))

	// Node status section.
	sb.WriteString("Node status\n")
	sb.WriteString("---------------------------------------------------------------\n")

	// Active nodes.
	sb.WriteString("Active:\n")
	if len(activeNodes) == 0 {
		sb.WriteString(" (no active nodes)\n")
	} else {
		nodeGroups := make([]string, 0, len(activeNodes))
		for nodeGroup := range activeNodes {
			nodeGroups = append(nodeGroups, nodeGroup)
		}
		sort.Strings(nodeGroups)
		for _, nodeGroup := range nodeGroups {
			count := activeNodes[nodeGroup]
			sb.WriteString(fmt.Sprintf(" %d %s\n", count, nodeGroup))
		}
	}

	// Pending nodes (we do not have this info from node_events).
	sb.WriteString("Pending:\n")
	sb.WriteString(" (no pending nodes)\n")

	// Recent failures.
	sb.WriteString("Recent failures:\n")
	if len(failedNodes) == 0 {
		sb.WriteString(" (no failures)\n")
	} else {
		for _, nodeID := range failedNodes {
			sb.WriteString(fmt.Sprintf(" %s: NodeTerminated\n", nodeID))
		}
	}

	sb.WriteString("\n")

	// Resources section.
	sb.WriteString("Resources\n")
	sb.WriteString("---------------------------------------------------------------\n")

	// Usage.
	sb.WriteString("Usage:\n")
	if len(totalResources) == 0 {
		sb.WriteString(" (no resources)\n")
	} else {
		resources := make([]string, 0, len(totalResources))
		for resource := range totalResources {
			resources = append(resources, resource)
		}
		sort.Strings(resources)
		for _, resource := range resources {
			total := totalResources[resource]
			used := usedResources[resource]
			sb.WriteString(fmt.Sprintf(" %s/%s %s\n", formatResourceValue(resource, used), formatResourceValue(resource, total), resource))
		}
	}

	sb.WriteString("\n")

	// Demands.
	sb.WriteString("Demands:\n")
	if len(demands) == 0 {
		sb.WriteString(" (no resource demands)\n")
	} else {
		for _, demand := range demands {
			sb.WriteString(fmt.Sprintf(" %s: %d+ pending tasks/actors\n", formatResourceMap(demand.Resources), demand.Count))
		}
	}

	return sb.String()
}

// formatResourceValue formats a resource value appropriately (e.g., bytes to human readable).
func formatResourceValue(resource string, value float64) string {
	// Memory resources should be formatted as bytes.
	if strings.Contains(strings.ToLower(resource), "memory") {
		return utils.FormatBytes(value)
	}
	// CPU and other numeric resources.
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

func formatResourceMap(resources map[string]float64) string {
	if len(resources) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(resources))
	for k := range resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strconv.FormatFloat(resources[k], 'f', -1, 64)
		parts = append(parts, fmt.Sprintf("'%s': %s", k, v))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

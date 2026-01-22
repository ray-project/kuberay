package historyserver

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

const (
	timestampDisplayFormat = "2006-01-02 15:04:05.000000"
	sessionTimestampFormat = "2006-01-02_15-04-05"
)

// ResourceDemand represents a pending resource request with count
type ResourceDemand struct {
	Resources map[string]float64
	Count     int
}

// ClusterStatusBuilder aggregates data from multiple sources to build cluster status
type ClusterStatusBuilder struct {
	ActiveNodes map[string]int
	IdleNodes   map[string]int
	// We don't have pending node info from debug_state.txt
	// TODO FailedNodes would need node_events
	TotalResources map[string]float64
	UsedResources  map[string]float64
	PendingDemands []ResourceDemand // Demands from tasks/actors
	Timestamp      time.Time
}

// NewClusterStatusBuilder creates a new builder instance
func NewClusterStatusBuilder() *ClusterStatusBuilder {
	return &ClusterStatusBuilder{
		ActiveNodes:    make(map[string]int),
		IdleNodes:      make(map[string]int),
		TotalResources: make(map[string]float64),
		UsedResources:  make(map[string]float64),
		PendingDemands: []ResourceDemand{},
		// TODO: Use debug_state timestamp or storage metadata instead of session start time.
		Timestamp: time.Now(), // a fallback when parsing fails.
	}
}

// ParseSessionTimestamp returns the session timestamp encoded in sessionName.
// Expected format: "session_2006-01-02_15-04-05_123456"
func ParseSessionTimestamp(sessionName string) (time.Time, bool) {
	after, found := strings.CutPrefix(sessionName, "session_")
	if !found || len(after) < len(sessionTimestampFormat) {
		return time.Time{}, false
	}

	ts, err := time.Parse(sessionTimestampFormat, after[:len(sessionTimestampFormat)])
	if err != nil {
		return time.Time{}, false
	}

	return ts, true
}

// AddNodeFromDebugState adds node information from a parsed debug_state.txt
func (b *ClusterStatusBuilder) AddNodeFromDebugState(state *NodeDebugState) {
	if state == nil {
		return
	}

	nodeGroup := state.NodeGroup
	if nodeGroup == "" {
		nodeGroup = "default"
	}

	if state.IsIdle {
		b.IdleNodes[nodeGroup]++
	} else {
		b.ActiveNodes[nodeGroup]++
	}

	for key, value := range state.Total {
		b.TotalResources[key] += value
	}

	usedResources := state.GetUsedResources()
	for key, value := range usedResources {
		b.UsedResources[key] += value
	}
}

// AddPendingDemandsFromTasks calculates pending demands from tasks
func (b *ClusterStatusBuilder) AddPendingDemandsFromTasks(tasks []types.Task) {
	demandMap := make(map[string]*ResourceDemand)

	for _, task := range tasks {
		if !isPendingTaskState(task.State) || len(task.RequiredResources) == 0 {
			continue
		}

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

	b.mergeDemands(demandMap)
}

// AddPendingDemandsFromActors calculates pending demands from actors
func (b *ClusterStatusBuilder) AddPendingDemandsFromActors(actors []types.Actor) {
	demandMap := make(map[string]*ResourceDemand)

	for _, actor := range actors {
		if !isPendingActorState(actor.State) || len(actor.RequiredResources) == 0 {
			continue
		}

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

	b.mergeDemands(demandMap)
}

func (b *ClusterStatusBuilder) mergeDemands(demandMap map[string]*ResourceDemand) {
	indexByKey := make(map[string]int, len(b.PendingDemands))
	for i, existing := range b.PendingDemands {
		indexByKey[resourceKey(existing.Resources)] = i
	}

	for _, demand := range demandMap {
		key := resourceKey(demand.Resources)
		if idx, found := indexByKey[key]; found {
			b.PendingDemands[idx].Count += demand.Count
		} else {
			indexByKey[key] = len(b.PendingDemands)
			b.PendingDemands = append(b.PendingDemands, *demand)
		}
	}
}

func isPendingTaskState(state types.TaskStatus) bool {
	switch state {
	case types.PENDING_ARGS_AVAIL,
		types.PENDING_NODE_ASSIGNMENT,
		types.PENDING_OBJ_STORE_MEM_AVAIL,
		types.PENDING_ARGS_FETCH,
		types.SUBMITTED_TO_WORKER,
		types.PENDING_ACTOR_TASK_ARGS_FETCH,
		types.PENDING_ACTOR_TASK_ORDERING_OR_CONCURRENCY:
		return true
	default:
		return false
	}
}

func isPendingActorState(state types.StateType) bool {
	switch state {
	case types.PENDING_CREATION, types.DEPENDENCIES_UNREADY:
		return true
	default:
		return false
	}
}

// resourceKey returns a stable, order-independent signature for resource maps
// so equivalent demands can be grouped together.
func resourceKey(resources map[string]float64) string {
	if len(resources) == 0 {
		return ""
	}

	var parts []string
	for k, v := range resources {
		parts = append(parts, fmt.Sprintf("%s:%.2f", k, v))
	}

	slices.Sort(parts)

	return strings.Join(parts, ",")
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func formatResourceValue(resource string, value float64) string {
	if strings.Contains(strings.ToLower(resource), "memory") {
		return formatBytes(value)
	}

	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}

	return fmt.Sprintf("%.2f", value)
}

// formatBytes formats bytes as a human-readable string (e.g., "1.50GiB")
func formatBytes(bytes float64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%.0fB", bytes)
	}

	div, exp := float64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f%ciB", bytes/div, "KMGTPE"[exp])
}

func formatResourceMapForDisplay(resources map[string]float64) string {
	if len(resources) == 0 {
		return "{}"
	}

	var parts []string
	for _, k := range sortedKeys(resources) {
		v := resources[k]
		if v == float64(int64(v)) {
			parts = append(parts, fmt.Sprintf("'%s': %.0f", k, v))
		} else {
			parts = append(parts, fmt.Sprintf("'%s': %.2f", k, v))
		}
	}

	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

// FormatStatus formats the cluster status as a string matching Ray's format
func (b *ClusterStatusBuilder) FormatStatus() string {
	var sb strings.Builder

	// Header with timestamp
	sb.WriteString(fmt.Sprintf("======== Autoscaler status: %s ========\n",
		b.Timestamp.Format(timestampDisplayFormat)))

	// Node status section
	sb.WriteString("Node status\n")
	sb.WriteString("---------------------------------------------------------------\n")

	// Active nodes
	sb.WriteString("Active:\n")
	if len(b.ActiveNodes) == 0 {
		sb.WriteString(" (no active nodes)\n")
	} else {
		for _, group := range sortedKeys(b.ActiveNodes) {
			sb.WriteString(fmt.Sprintf(" %d %s\n", b.ActiveNodes[group], group))
		}
	}

	// Idle nodes
	sb.WriteString("Idle:\n")
	if len(b.IdleNodes) == 0 {
		sb.WriteString(" (no idle nodes)\n")
	} else {
		for _, group := range sortedKeys(b.IdleNodes) {
			sb.WriteString(fmt.Sprintf(" %d %s\n", b.IdleNodes[group], group))
		}
	}

	// Pending nodes (not available from debug_state.txt)
	sb.WriteString("Pending:\n")
	// TODO Reconstruct pending nodes when autoscaler state is archived.
	sb.WriteString(" (unavailable in history server)\n")

	// Recent failures (would need node_events - handled by another PR)
	sb.WriteString("Recent failures:\n")
	// TODO Reconstruct failures when node_events/autoscaler state is archived.
	sb.WriteString(" (unavailable in history server)\n")

	sb.WriteString("\n")

	// Resources section
	sb.WriteString("Resources\n")
	sb.WriteString("---------------------------------------------------------------\n")

	// Total Usage
	sb.WriteString("Total Usage:\n")
	if len(b.TotalResources) == 0 {
		sb.WriteString(" (no resources)\n")
	} else {
		for _, key := range sortedKeys(b.TotalResources) {
			total := b.TotalResources[key]
			used := b.UsedResources[key]
			sb.WriteString(fmt.Sprintf(" %s/%s %s\n",
				formatResourceValue(key, used),
				formatResourceValue(key, total),
				key))
		}
	}

	sb.WriteString("\n")

	sb.WriteString("From request_resources:\n")
	// TODO Reconstruct request_resources when autoscaler demand estimator is archived.
	sb.WriteString(" (unavailable in history server)\n")

	// Pending Demands
	sb.WriteString("Pending Demands:\n")
	if len(b.PendingDemands) == 0 {
		sb.WriteString(" (no resource demands)")
	} else {
		for _, demand := range b.PendingDemands {
			sb.WriteString(fmt.Sprintf(" %s: %d+ pending tasks/actors\n",
				formatResourceMapForDisplay(demand.Resources), demand.Count))
		}
	}

	return sb.String()
}

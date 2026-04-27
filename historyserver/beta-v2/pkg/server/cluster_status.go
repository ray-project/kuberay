package server

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	// Ray scales resource values as int64 = actual × 10000 (FixedPoint format;
	// src/ray/common/constants.h).
	rayResourceScale = 10000.0

	// Formatting constants. Must match Ray's util.py output exactly so the
	// frontend (which renders the string raw in a <pre>) stays visually stable.
	timestampDisplayFormat = "2006-01-02 15:04:05.000000"
	sessionTimestampFormat = "2006-01-02_15-04-05"

	// Cap on failed nodes displayed (matches Ray's AUTOSCALER_MAX_FAILURES_DISPLAYED).
	maxFailuresDisplayed = 20
)

// Regexes for Ray-produced text in debug_state.txt; formats are stable
// because Ray emits them.
var (
	reNodeGroup  = regexp.MustCompile(`"ray\.io/node-group"\s*:\s*"([^"]+)"`)
	reTotal      = regexp.MustCompile(`"total"\s*:\s*\{([^}]+)\}`)
	reAvailable  = regexp.MustCompile(`"available"\s*:\s*\{([^}]+)\}`)
	reResourceKV = regexp.MustCompile(`([a-zA-Z0-9_:./\-]+):\s*\[([0-9,\s]+)\]`)
)

// nodeDebugState is the parsed form of a single node's debug_state.txt.
type nodeDebugState struct {
	NodeID    string
	NodeName  string
	NodeGroup string
	IsIdle    bool
	Total     map[string]float64
	Available map[string]float64
}

// getUsedResources returns max(0, total - available) per resource.
func (s *nodeDebugState) getUsedResources() map[string]float64 {
	used := make(map[string]float64, len(s.Total))
	for key, total := range s.Total {
		used[key] = math.Max(0, total-s.Available[key])
	}
	return used
}

// resourceDemand represents a pending resource request and its count.
type resourceDemand struct {
	Resources map[string]float64
	Count     int
}

// failedNode captures a node that transitioned to DEAD with a non-expected reason.
type failedNode struct {
	NodeID        string
	NodeIPAddress string
	InstanceID    string // empty until Ray exports it upstream
	NodeType      string // labels["ray.io/node-group"] with NodeID fallback
	Timestamp     time.Time
}

// clusterStatusBuilder accumulates the inputs needed to emit FormatStatus.
type clusterStatusBuilder struct {
	ActiveNodes    map[string]int
	IdleNodes      map[string]int
	TotalResources map[string]float64
	UsedResources  map[string]float64
	FailedNodes    []failedNode
	PendingDemands []resourceDemand
	Timestamp      time.Time
}

func newClusterStatusBuilder() *clusterStatusBuilder {
	return &clusterStatusBuilder{
		ActiveNodes:    make(map[string]int),
		IdleNodes:      make(map[string]int),
		TotalResources: make(map[string]float64),
		UsedResources:  make(map[string]float64),
	}
}

// buildFormattedClusterStatus reads debug_state.txt for every node and merges
// it with snapshot data into the Ray-formatted status block.
func buildFormattedClusterStatus(
	reader readerLike,
	snap *snapshot.SessionSnapshot,
	clusterNameID, sessionName string,
) string {
	builder := newClusterStatusBuilder()

	// Step 1: enumerate node IDs by listing logs/. The directory listing is
	// the authoritative node roster — every alive node writes debug_state.txt.
	logsPath := path.Join(sessionName, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	nodeIDs := reader.ListFiles(clusterNameID, logsPath)

	// Step 2: parse each debug_state.txt and fold it into the builder. Skipping
	// unparseable files is intentional for historical (possibly truncated) data.
	for _, nodeID := range nodeIDs {
		// ListFiles returns directory entries; strip a trailing "/" if present.
		nodeID = strings.TrimSuffix(nodeID, "/")
		debugStatePath := path.Join(logsPath, nodeID, "debug_state.txt")

		r := reader.GetContent(clusterNameID, debugStatePath)
		if r == nil {
			continue
		}
		state, err := parseDebugState(r)
		if err != nil {
			continue
		}
		builder.addNodeFromDebugState(state)
	}

	// Step 3: pick the cluster's "last active" timestamp — latest task/actor
	// EndTime, then session start time from sessionName, else zero (formatter
	// prints "time unknown").
	tasks := flattenTasks(snap.Tasks)
	actors := make([]eventtypes.Actor, 0, len(snap.Actors))
	for _, a := range snap.Actors {
		actors = append(actors, a)
	}
	if ts := getLastTimestamp(tasks, actors); !ts.IsZero() {
		builder.Timestamp = ts
	} else if ts := parseSessionTimestamp(sessionName); !ts.IsZero() {
		builder.Timestamp = ts
	}

	// Step 4: pull in dead-node failures and pending demands from snapshot.
	builder.addFailedNodesFromNodes(snap.Nodes)
	builder.addPendingDemandsFromTasks(tasks)
	builder.addPendingDemandsFromActors(actors)

	return builder.formatStatus()
}

// readerLike is the subset of storage.StorageReader the cluster-status logic uses.
type readerLike interface {
	ListFiles(clusterID, dir string) []string
	GetContent(clusterID, fileName string) io.Reader
}

// --- debug_state.txt parsing -------------------------------------------------

// parseDebugState reads Ray's debug_state.txt and extracts node metadata
// and the cluster resource scheduler state. The file is emitted by Ray's
// Raylet (ClusterResourceScheduler::DebugString); the line we parse looks
// like:
//
//	cluster_resource_scheduler state:
//	Local id: <id> Local resources: { ... "total":{...}, "available":{...} ... }
func parseDebugState(rd io.Reader) (*nodeDebugState, error) {
	state := &nodeDebugState{
		Total:     make(map[string]float64),
		Available: make(map[string]float64),
	}

	reader := bufio.NewReader(rd)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		switch {
		case strings.HasPrefix(line, "Node ID:"):
			state.NodeID = strings.TrimSpace(strings.TrimPrefix(line, "Node ID:"))
		case strings.HasPrefix(line, "Node name:"):
			state.NodeName = strings.TrimSpace(strings.TrimPrefix(line, "Node name:"))
		case strings.Contains(line, "cluster_resource_scheduler state:"):
			// The actual data is on the NEXT line — read once more and parse.
			nextLine, nextErr := reader.ReadString('\n')
			if nextErr != nil && nextErr != io.EOF {
				return nil, nextErr
			}
			parseClusterResourceSchedulerLine(nextLine, state)
			return state, nil
		}

		if err == io.EOF {
			break
		}
	}

	return state, nil
}

// parseClusterResourceSchedulerLine extracts is_idle, node group, and the
// total/available resource maps from a single encoded line.
func parseClusterResourceSchedulerLine(line string, state *nodeDebugState) {
	state.IsIdle = strings.Contains(line, "is_idle: 1")

	if m := reNodeGroup.FindStringSubmatch(line); len(m) > 1 {
		state.NodeGroup = m[1]
	}
	if m := reTotal.FindStringSubmatch(line); len(m) > 1 {
		state.Total = parseResourceMap(m[1])
	}
	if m := reAvailable.FindStringSubmatch(line); len(m) > 1 {
		state.Available = parseResourceMap(m[1])
	}
}

// parseResourceMap parses a "key: [v1, v2, ...]" blob (FixedPointVectorToString
// output from Ray) into a map[string]float64, rescaling the FixedPoint
// multiplier. Node-specific resources (e.g. "node:10.0.0.1") are dropped —
// they do not belong in the aggregate display.
func parseResourceMap(resourceStr string) map[string]float64 {
	resources := make(map[string]float64)

	for _, match := range reResourceKV.FindAllStringSubmatch(resourceStr, -1) {
		if len(match) <= 2 {
			continue
		}
		key := strings.TrimSpace(match[1])
		if strings.HasPrefix(key, "node:") {
			continue
		}

		var total float64
		for _, part := range strings.Split(match[2], ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, err := strconv.ParseFloat(part, 64)
			if err != nil {
				continue
			}
			total += v
		}
		resources[key] = total / rayResourceScale
	}
	return resources
}

// --- snapshot → builder helpers ---------------------------------------------

// addNodeFromDebugState increments per-group counters and resource totals.
func (b *clusterStatusBuilder) addNodeFromDebugState(state *nodeDebugState) {
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
	for k, v := range state.Total {
		b.TotalResources[k] += v
	}
	for k, v := range state.getUsedResources() {
		b.UsedResources[k] += v
	}
}

// addFailedNodesFromNodes extracts DEAD transitions (excluding
// EXPECTED_TERMINATION / AUTOSCALER_DRAIN_IDLE) — those are user-driven, not
// failures. Each node contributes at most once (latest DEAD transition), and
// the overall list is sorted by timestamp desc and capped at
// maxFailuresDisplayed.
func (b *clusterStatusBuilder) addFailedNodesFromNodes(nodes map[string]eventtypes.Node) {
	for _, node := range nodes {
		for i := len(node.StateTransitions) - 1; i >= 0; i-- {
			tr := node.StateTransitions[i]
			if tr.State != eventtypes.NODE_DEAD {
				continue
			}
			if tr.DeathInfo != nil &&
				(tr.DeathInfo.Reason == eventtypes.EXPECTED_TERMINATION ||
					tr.DeathInfo.Reason == eventtypes.AUTOSCALER_DRAIN_IDLE) {
				continue
			}

			// node-group label is the autoscaler identity; fall back to node ID when missing.
			nodeType := node.Labels["ray.io/node-group"]
			if nodeType == "" {
				nodeType = node.NodeID
			}
			b.FailedNodes = append(b.FailedNodes, failedNode{
				NodeID:        node.NodeID,
				NodeIPAddress: node.NodeIPAddress,
				InstanceID:    node.InstanceID,
				NodeType:      nodeType,
				Timestamp:     tr.Timestamp,
			})
			break
		}
	}
	slices.SortFunc(b.FailedNodes, func(a, bx failedNode) int {
		return bx.Timestamp.Compare(a.Timestamp)
	})
	if len(b.FailedNodes) > maxFailuresDisplayed {
		b.FailedNodes = b.FailedNodes[:maxFailuresDisplayed]
	}
}

// addPendingDemandsFromTasks adds an entry per unique required-resource shape
// across tasks that are still in a pending state. Resource shapes are matched
// by a deterministic signature so equivalent demands are collapsed.
func (b *clusterStatusBuilder) addPendingDemandsFromTasks(tasks []eventtypes.Task) {
	demandMap := make(map[string]*resourceDemand)
	for _, task := range tasks {
		if !isPendingTaskState(task.State) || len(task.RequiredResources) == 0 {
			continue
		}
		key := resourceKey(task.RequiredResources)
		if key == "" {
			continue
		}
		if _, exists := demandMap[key]; !exists {
			demandMap[key] = &resourceDemand{
				Resources: task.RequiredResources,
				Count:     0,
			}
		}
		demandMap[key].Count++
	}
	b.mergeDemands(demandMap)
}

// addPendingDemandsFromActors is the actor-side equivalent of
// addPendingDemandsFromTasks. Same shape-matching rules apply.
func (b *clusterStatusBuilder) addPendingDemandsFromActors(actors []eventtypes.Actor) {
	demandMap := make(map[string]*resourceDemand)
	for _, actor := range actors {
		if !isPendingActorState(actor.State) || len(actor.RequiredResources) == 0 {
			continue
		}
		key := resourceKey(actor.RequiredResources)
		if key == "" {
			continue
		}
		if _, exists := demandMap[key]; !exists {
			demandMap[key] = &resourceDemand{
				Resources: actor.RequiredResources,
				Count:     0,
			}
		}
		demandMap[key].Count++
	}
	b.mergeDemands(demandMap)
}

// mergeDemands folds a fresh demandMap into the builder's PendingDemands,
// summing counts when the resource shape already exists.
func (b *clusterStatusBuilder) mergeDemands(demandMap map[string]*resourceDemand) {
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

// --- pending-state classification ---------------------------------------------

func isPendingTaskState(state eventtypes.TaskStatus) bool {
	switch state {
	case eventtypes.PENDING_ARGS_AVAIL,
		eventtypes.PENDING_NODE_ASSIGNMENT,
		eventtypes.PENDING_OBJ_STORE_MEM_AVAIL,
		eventtypes.PENDING_ARGS_FETCH:
		return true
	}
	return false
}

func isPendingActorState(state eventtypes.StateType) bool {
	switch state {
	case eventtypes.PENDING_CREATION, eventtypes.DEPENDENCIES_UNREADY:
		return true
	}
	return false
}

// resourceKey produces a stable, order-independent signature for grouping
// equivalent resource demands. 0.0001 precision matches Ray's fractional
// resource minimum.
func resourceKey(resources map[string]float64) string {
	if len(resources) == 0 {
		return ""
	}
	parts := make([]string, 0, len(resources))
	for k, v := range resources {
		parts = append(parts, fmt.Sprintf("%s:%.4f", k, v))
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

// --- timestamp helpers --------------------------------------------------------

// getLastTimestamp returns the latest EndTime observed across tasks + actors,
// which we use as a proxy for "when the cluster was last active."
func getLastTimestamp(tasks []eventtypes.Task, actors []eventtypes.Actor) time.Time {
	var latest time.Time
	for _, t := range tasks {
		if t.EndTime.After(latest) {
			latest = t.EndTime
		}
	}
	for _, a := range actors {
		if a.EndTime.After(latest) {
			latest = a.EndTime
		}
	}
	return latest
}

// parseSessionTimestamp extracts the leading datetime out of Ray session names
// of the form "session_YYYY-MM-DD_HH-MM-SS_<suffix>". Returns zero Time on
// parse failure (caller falls back to "time unknown").
func parseSessionTimestamp(sessionName string) time.Time {
	rest, ok := strings.CutPrefix(sessionName, "session_")
	if !ok {
		return time.Time{}
	}
	parts := strings.Split(rest, "_")
	if len(parts) < 2 {
		return time.Time{}
	}
	tsStr := parts[0] + "_" + parts[1]
	ts, err := time.Parse(sessionTimestampFormat, tsStr)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// --- formatting helpers -------------------------------------------------------

// sortedKeys returns the map's keys in lexicographic order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// formatResourceValue formats a single resource value matching Python's
// behavior: memory resources via formatMemory, others via formatPythonFloat.
func formatResourceValue(resource string, value float64) string {
	if strings.Contains(strings.ToLower(resource), "memory") {
		return formatMemory(value)
	}
	return formatPythonFloat(value)
}

// formatResourceMapForDisplay mimics Python's dict repr for the autoscaler's
// "Pending Demands" line (e.g. "{'CPU': 1.0, 'memory': 1073741824.0}").
func formatResourceMapForDisplay(resources map[string]float64) string {
	if len(resources) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(resources))
	for _, k := range sortedKeys(resources) {
		parts = append(parts, fmt.Sprintf("'%s': %s", k, formatPythonFloat(resources[k])))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

// formatPythonFloat rounds to Python's default float repr: integers keep ".0",
// fractions use -1 precision to drop trailing zeros.
func formatPythonFloat(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.1f", value)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// formatStatus emits the final autoscaler status block (timestamp, node
// groups, failures, resources, pending demands).
func (b *clusterStatusBuilder) formatStatus() string {
	var sb strings.Builder

	timestampStr := "time unknown"
	if !b.Timestamp.IsZero() {
		timestampStr = b.Timestamp.Format(timestampDisplayFormat)
	}
	header := fmt.Sprintf("======== Autoscaler status: %s ========", timestampStr)
	separator := strings.Repeat("-", len(header))
	sb.WriteString(header + "\n")

	sb.WriteString("Node status\n")
	sb.WriteString(separator + "\n")

	sb.WriteString("Active:\n")
	if len(b.ActiveNodes) == 0 {
		sb.WriteString(" (no active nodes)\n")
	} else {
		for _, group := range sortedKeys(b.ActiveNodes) {
			sb.WriteString(fmt.Sprintf(" %d %s\n", b.ActiveNodes[group], group))
		}
	}

	sb.WriteString("Idle:\n")
	if len(b.IdleNodes) == 0 {
		sb.WriteString(" (no idle nodes)\n")
	} else {
		for _, group := range sortedKeys(b.IdleNodes) {
			sb.WriteString(fmt.Sprintf(" %d %s\n", b.IdleNodes[group], group))
		}
	}

	sb.WriteString("Pending:\n")
	// Pending nodes come from autoscaler events; not yet exported.
	sb.WriteString(" (unavailable in history server)\n")

	sb.WriteString("Recent failures:\n")
	if len(b.FailedNodes) == 0 {
		sb.WriteString(" (no failures)\n")
	} else {
		for _, node := range b.FailedNodes {
			if node.InstanceID != "" {
				sb.WriteString(fmt.Sprintf(" %s: NodeTerminated (instance_id: %s)\n", node.NodeType, node.InstanceID))
			} else {
				sb.WriteString(fmt.Sprintf(" %s: NodeTerminated (ip: %s)\n", node.NodeType, node.NodeIPAddress))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString("Resources\n")
	sb.WriteString(separator + "\n")

	sb.WriteString("Total Usage:\n")
	if len(b.TotalResources) == 0 {
		sb.WriteString(" (no resources)\n")
	} else {
		for _, key := range sortedKeys(b.TotalResources) {
			// accelerator_type: is metadata, not a real resource — suppress
			// to match Ray's non-verbose display.
			if strings.HasPrefix(key, "accelerator_type:") {
				continue
			}
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
	sb.WriteString(" (unavailable in history server)\n")

	sb.WriteString("Pending Demands:\n")
	if len(b.PendingDemands) == 0 {
		// NOTE: Bug-for-bug match with v1. If v1 gets fixed, we should fix this too.
		sb.WriteString(" (no resource demands)")
	} else {
		for _, demand := range b.PendingDemands {
			sb.WriteString(fmt.Sprintf(" %s: %d+ pending tasks/actors\n",
				formatResourceMapForDisplay(demand.Resources), demand.Count))
		}
	}

	return sb.String()
}

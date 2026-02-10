package utils

import (
	"math"
	"sort"
	"strings"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/sirupsen/logrus"
)

// --- Lineage Summary Types ---

// Link represents a navigation reference to a task or actor detail page
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L994-L998
type Link struct {
	Type string `json:"type"` // task or actor
	ID   string `json:"id"`   // task ID or actor ID
}

// NestedTaskSummary represents an entry in the task lineage tree.
// An entry can be:
// - a task (type = NORMAL_TASK, ACTOR_TASK, or ACTOR_CREATION_TASK)
// - an actor grouping its creation and method tasks (type = ACTOR)
// - a group of same-named siblings, created when >1 sibling shares a name (type = GROUP)
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L1001-L1018
type NestedTaskSummary struct {
	Name        string               `json:"name"`
	Key         string               `json:"key"`
	Type        string               `json:"type"`
	Timestamp   *int64               `json:"timestamp"`
	StateCounts map[string]int       `json:"state_counts"`
	Children    []*NestedTaskSummary `json:"children"`
	Link        *Link                `json:"link,omitempty"`
}

// TaskSummaries is the response for summary_by=lineage
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L1022-L1033
type TaskSummaries struct {
	Summary             []*NestedTaskSummary `json:"summary"`
	TotalTasks          int                  `json:"total_tasks"`
	TotalActorTasks     int                  `json:"total_actor_tasks"`
	TotalActorScheduled int                  `json:"total_actor_scheduled"`
	SummaryBy           string               `json:"summary_by"`
}

// DriverTaskIDPrefix is the hex prefix of the driver's task ID (20 bytes of 0xFF).
// In Ray, tasks whose parent_task_id starts with this prefix are spawned directly
// by the driver and should be placed at the root level of the lineage tree.
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L979
const DriverTaskIDPrefix = "ffffffffffffffffffffffffffffffffffffffff"

// actorCreationTaskIDNilPrefix is the hex representation of the 8-byte nil unique bytes
// that Ray uses as the prefix of ACTOR_CREATION_TASK task IDs.
// Format: task_id = ffffffffffffffff{actorID} (8 bytes nil + 16 bytes ActorID)
// Ref: https://github.com/ray-project/ray/blob/36be009ae360788550e541d81806493f52963730/src/ray/common/id.cc#L171-L176
const actorCreationTaskIDNilPrefix = "ffffffffffffffff"

// lineageBuilder encapsulates the state needed to build a lineage tree.
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L1098-L1118
type lineageBuilder struct {
	tasksByID           map[string]eventtypes.Task  // taskID -> Task
	actorsByID          map[string]eventtypes.Actor // actorID -> Actor for name resolution
	actorCreationTaskID map[string]string           // actorID -> creation taskID (to find actor's parent in lineage)
	taskGroupByID       map[string]*NestedTaskSummary
	rootSummary         []*NestedTaskSummary
	totalTasks          int // NORMAL_TASK count
	totalActorTasks     int // ACTOR_TASK count
	totalActorScheduled int // ACTOR_CREATION_TASK count
}

// isDriverTaskID checks if the given taskID belongs to a driver task.
// Task IDs are already normalized to hex by normalizeTaskIDsToHex during event ingestion,
// so we simply check the hex prefix directly.
func isDriverTaskID(taskID string) bool {
	return taskID != "" && strings.HasPrefix(taskID, DriverTaskIDPrefix)
}

// BuildLineageSummary constructs a hierarchical task summary following Ray's lineage algorithm.
// The algorithm has 5 steps:
//  1. Index all tasks by ID and track actor creation tasks
//  2. Build tree structure based on task ownership (actor tasks -> actor entry, others -> parent task)
//  3. Merge siblings with the same name into GROUP nodes
//  4. Calculate total state_counts by summing children recursively
//  5. Sort by running > pending > failed > timestamp > actor_creation
//
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1075-L1375
func BuildLineageSummary(tasks []eventtypes.Task, actors []eventtypes.Actor) *TaskSummaries {
	b := &lineageBuilder{
		tasksByID:           make(map[string]eventtypes.Task),
		actorsByID:          make(map[string]eventtypes.Actor),
		actorCreationTaskID: make(map[string]string),
		taskGroupByID:       make(map[string]*NestedTaskSummary),
		rootSummary:         make([]*NestedTaskSummary, 0),
	}

	// Step 1: Index
	b.indexData(tasks, actors)

	// Step 2: Build tree
	b.buildTree(tasks)

	// Step 3: Merge siblings
	b.rootSummary = b.mergeSiblings(b.rootSummary)

	// Step 4 & 5: Calculate totals and sort
	b.calculateTotals(b.rootSummary)
	b.sortGroups(b.rootSummary)

	logrus.Debugf("Built lineage summary: %d root nodes, %d tasks, %d actor tasks, %d actors",
		len(b.rootSummary), b.totalTasks, b.totalActorTasks, b.totalActorScheduled)

	return &TaskSummaries{
		Summary:             b.rootSummary,
		TotalTasks:          b.totalTasks,
		TotalActorTasks:     b.totalActorTasks,
		TotalActorScheduled: b.totalActorScheduled,
		SummaryBy:           "lineage",
	}
}

// indexData builds lookup maps to access during tree construction.
// Also tracks ACTOR_CREATION_TASK -> actorID mapping to determine actor ownership in the tree.
func (b *lineageBuilder) indexData(tasks []eventtypes.Task, actors []eventtypes.Actor) {
	for i, task := range tasks {
		// ACTOR_CREATION_TASK from TASK_DEFINITION_EVENT has no ActorID.
		// Derive it from task_id: ffffffffffffffff{actorID}
		// Ref: https://github.com/ray-project/ray/blob/36be009ae360788550e541d81806493f52963730/src/ray/common/id.cc#L171-L176
		if task.TaskType == eventtypes.ACTOR_CREATION_TASK && task.ActorID == "" {
			if strings.HasPrefix(task.TaskID, actorCreationTaskIDNilPrefix) {
				tasks[i].ActorID = task.TaskID[len(actorCreationTaskIDNilPrefix):]
				task = tasks[i]
			}
		}
		b.tasksByID[task.TaskID] = task
		if task.TaskType == eventtypes.ACTOR_CREATION_TASK && task.ActorID != "" {
			b.actorCreationTaskID[task.ActorID] = task.TaskID
		}
	}
	for _, actor := range actors {
		b.actorsByID[actor.ActorID] = actor
	}
}

// buildTree iterates through all tasks, creates tree nodes, and updates state counts.
// NOTE: DRIVER_TASK is skipped because Ray's live Dashboard API excludes them from lineage.
func (b *lineageBuilder) buildTree(tasks []eventtypes.Task) {
	for _, task := range tasks {
		if task.TaskType == eventtypes.DRIVER_TASK {
			continue
		}

		group := b.getOrCreateTaskGroup(task.TaskID)
		if group == nil {
			continue
		}

		state := string(task.State)
		if state == "" {
			state = "UNKNOWN"
		}
		group.StateCounts[state]++

		switch task.TaskType {
		case eventtypes.NORMAL_TASK:
			b.totalTasks++
		case eventtypes.ACTOR_CREATION_TASK:
			b.totalActorScheduled++
		case eventtypes.ACTOR_TASK:
			b.totalActorTasks++
		}
	}
}

// getOrCreateTaskGroup returns the existing NestedTaskSummary for a task, or creates one.
//
// Parent assignment rules:
// - ACTOR_TASK / ACTOR_CREATION_TASK -> parent is the ACTOR
// - NORMAL_TASK with driver parent or no parent -> root level
// - NORMAL_TASK with valid parent -> nested under parent task
//
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1120-L1177
func (b *lineageBuilder) getOrCreateTaskGroup(taskID string) *NestedTaskSummary {
	if existing, ok := b.taskGroupByID[taskID]; ok {
		return existing
	}

	task, ok := b.tasksByID[taskID]
	if !ok {
		logrus.Debugf("Missing task data for %s", taskID)
		return nil
	}

	// Prefer user-defined task name, fallback to function call string to match Ray Dashboard
	name := task.GetTaskName()
	if name == "" {
		name = task.GetFuncName()
	}

	var timestamp *int64
	if !task.CreationTime.IsZero() {
		ts := task.CreationTime.UnixMilli()
		timestamp = &ts
	}

	group := &NestedTaskSummary{
		Name:        name,
		Key:         taskID,
		Type:        string(task.TaskType),
		Timestamp:   timestamp,
		StateCounts: make(map[string]int),
		Children:    make([]*NestedTaskSummary, 0),
		Link:        &Link{Type: "task", ID: taskID},
	}
	b.taskGroupByID[taskID] = group

	// Determine parent based on task type:
	// Actor-related tasks are grouped under their ACTOR entry, not their parent task.
	if task.TaskType == eventtypes.ACTOR_TASK || task.TaskType == eventtypes.ACTOR_CREATION_TASK {
		parent := b.getOrCreateActorGroup(task.ActorID)
		if parent != nil {
			parent.Children = append(parent.Children, group)
		}
	} else {
		// Normal tasks: place under parent task, or at root if parent is driver/missing
		parentID := task.ParentTaskID
		if parentID == "" || isDriverTaskID(parentID) {
			// No parent or parent is driver -> root level
			b.rootSummary = append(b.rootSummary, group)
		} else {
			parent := b.getOrCreateTaskGroup(parentID)
			if parent != nil {
				parent.Children = append(parent.Children, group)
			}
		}
	}

	return group
}

// getOrCreateActorGroup returns the existing ACTOR entry, or creates one.
// The ACTOR entry acts as a container for all actor-related tasks (creation + method calls).
// Its position in the tree is determined by the creation task's parent, matching Ray's behavior.
//
// Actor name resolution order: ReprName -> ActorClass -> creation task's GetFuncName() -> "UnknownActor"
//
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1179-L1236
func (b *lineageBuilder) getOrCreateActorGroup(actorID string) *NestedTaskSummary {
	key := "actor:" + actorID
	if existing, ok := b.taskGroupByID[key]; ok {
		return existing
	}

	// Find actor name: prefer ReprName, fallback to ActorClass
	var actorName string
	if actor, ok := b.actorsByID[actorID]; ok {
		actorName = actor.ReprName
		if actorName == "" {
			actorName = actor.ActorClass
		}
	}

	// Fallback: extract class name from creation task's function name (e.g., "Counter.__init__" -> "Counter")
	if actorName == "" {
		if creationTaskID, ok := b.actorCreationTaskID[actorID]; ok {
			if creationTask, ok := b.tasksByID[creationTaskID]; ok {
				funcName := creationTask.GetFuncName()
				if funcName != "" {
					parts := strings.Split(funcName, ".")
					actorName = parts[0]
				}
			}
		}
	}
	if actorName == "" {
		actorName = "UnknownActor"
	}

	// Get timestamp from creation task
	var timestamp *int64
	if creationTaskID, ok := b.actorCreationTaskID[actorID]; ok {
		if creationTask, ok := b.tasksByID[creationTaskID]; ok {
			if !creationTask.CreationTime.IsZero() {
				ts := creationTask.CreationTime.UnixMilli()
				timestamp = &ts
			}
		}
	}

	group := &NestedTaskSummary{
		Name:        actorName,
		Key:         key,
		Type:        "ACTOR",
		Timestamp:   timestamp,
		StateCounts: make(map[string]int),
		Children:    make([]*NestedTaskSummary, 0),
		Link:        &Link{Type: "actor", ID: actorID},
	}
	b.taskGroupByID[key] = group

	// Determine ACTOR entry's parent: same as its creation task's parent to match Ray's tree structure
	if creationTaskID, ok := b.actorCreationTaskID[actorID]; ok {
		if creationTask, ok := b.tasksByID[creationTaskID]; ok {
			parentID := creationTask.ParentTaskID
			if parentID == "" || isDriverTaskID(parentID) {
				b.rootSummary = append(b.rootSummary, group)
			} else {
				parent := b.getOrCreateTaskGroup(parentID)
				if parent != nil {
					parent.Children = append(parent.Children, group)
				}
			}
		}
	}

	return group
}

// mergeSiblings groups children with the same name into GROUP nodes.
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1261-L1311
func (b *lineageBuilder) mergeSiblings(siblings []*NestedTaskSummary) []*NestedTaskSummary {
	if len(siblings) == 0 {
		return siblings
	}

	// First, recursively merge children
	for _, child := range siblings {
		child.Children = b.mergeSiblings(child.Children)
	}

	// Group by name, preserving insertion order
	groups := make(map[string][]*NestedTaskSummary)
	order := make([]string, 0)
	for _, child := range siblings {
		if _, exists := groups[child.Name]; !exists {
			order = append(order, child.Name)
		}
		groups[child.Name] = append(groups[child.Name], child)
	}

	// Build result
	result := make([]*NestedTaskSummary, 0, len(order))
	for _, name := range order {
		members := groups[name]
		if len(members) == 1 {
			// Single child: keep as-is
			result = append(result, members[0])
		} else {
			// Multiple children with same name: create GROUP node
			var minTimestamp *int64
			for _, m := range members {
				if m.Timestamp != nil {
					if minTimestamp == nil || *m.Timestamp < *minTimestamp {
						minTimestamp = m.Timestamp
					}
				}
			}
			groupNode := &NestedTaskSummary{
				Name:        name,
				Key:         name, // GROUP uses name as key
				Type:        "GROUP",
				Timestamp:   minTimestamp,
				StateCounts: make(map[string]int),
				Children:    members,
				// GROUP nodes don't have a link
			}
			result = append(result, groupNode)
		}
	}

	return result
}

// calculateTotals recursively sums children's state_counts into parent nodes.
// This allows parent/GROUP nodes to display aggregated state information
// (e.g., a GROUP shows "3 RUNNING, 2 FINISHED" from its children).
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1343-L1363
func (b *lineageBuilder) calculateTotals(groups []*NestedTaskSummary) {
	for _, group := range groups {
		if len(group.Children) > 0 {
			// First, calculate totals for children
			b.calculateTotals(group.Children)

			// Then, sum children's state_counts into this group
			for _, child := range group.Children {
				for state, count := range child.StateCounts {
					group.StateCounts[state] += count
				}
			}
		}
	}
}

// sortGroups sorts nodes to surface the most important tasks first.
// Priority: running > pending > failed > earliest timestamp > actor_creation.
// This matches Ray Dashboard's sort order so users see active tasks at the top.
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1331-L1341
func (b *lineageBuilder) sortGroups(groups []*NestedTaskSummary) {
	// First, recursively sort children
	for _, group := range groups {
		if len(group.Children) > 0 {
			b.sortGroups(group.Children)
		}
	}

	// Sort this level
	sort.SliceStable(groups, func(i, j int) bool {
		// 1. Running tasks first (descending)
		runI := countRunning(groups[i])
		runJ := countRunning(groups[j])
		if runI != runJ {
			return runI > runJ
		}

		// 2. Pending tasks (descending)
		pendI := countPending(groups[i])
		pendJ := countPending(groups[j])
		if pendI != pendJ {
			return pendI > pendJ
		}

		// 3. Failed tasks (descending)
		failI := groups[i].StateCounts["FAILED"]
		failJ := groups[j].StateCounts["FAILED"]
		if failI != failJ {
			return failI > failJ
		}

		// 4. Timestamp (ascending - earlier first)
		tsI := getTimestamp(groups[i])
		tsJ := getTimestamp(groups[j])
		if tsI != tsJ {
			return tsI < tsJ
		}

		// 5. ACTOR_CREATION_TASK before others
		return groups[i].Type == "ACTOR_CREATION_TASK" && groups[j].Type != "ACTOR_CREATION_TASK"
	})
}

// countRunning returns the total count of all running sub-states.
// Ray has multiple running states (RUNNING, RUNNING_IN_RAY_GET, RUNNING_IN_RAY_WAIT).
func countRunning(g *NestedTaskSummary) int {
	return g.StateCounts["RUNNING"] +
		g.StateCounts["RUNNING_IN_RAY_GET"] +
		g.StateCounts["RUNNING_IN_RAY_WAIT"]
}

// countPending returns the total count of all pending sub-states.
// Ray has multiple pending states depending on what the task is waiting for.
func countPending(g *NestedTaskSummary) int {
	return g.StateCounts["PENDING_ARGS_AVAIL"] +
		g.StateCounts["PENDING_NODE_ASSIGNMENT"] +
		g.StateCounts["PENDING_OBJ_STORE_MEM_AVAIL"] +
		g.StateCounts["PENDING_ARGS_FETCH"]
}

// getTimestamp returns the timestamp, or MaxInt64 if nil (to sort unstarted tasks last).
func getTimestamp(g *NestedTaskSummary) int64 {
	if g.Timestamp == nil {
		return math.MaxInt64
	}
	return *g.Timestamp
}

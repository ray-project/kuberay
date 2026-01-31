package historyserver

import (
	"math"
	"sort"
	"strings"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

// DriverTaskIDPrefix identifies tasks spawned directly by the driver.
// Tasks with parent starting with this prefix should be placed at root level.
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L979
// Note: This is the hex representation. Ray events use base64 encoding.
const DriverTaskIDPrefix = "ffffffffffffffffffffffffffffffffffffffff"

// isDriverTaskID checks if the given taskID (base64 encoded) is a driver task.
// Converts base64 to hex and checks if it starts with DriverTaskIDPrefix.
func isDriverTaskID(taskID string) bool {
	if taskID == "" {
		return false
	}
	hexID, err := utils.ConvertBase64ToHex(taskID)
	if err != nil {
		// If conversion fails, fall back to checking base64 prefix
		// Base64 encoding of 0xFF bytes results in '/' characters
		return strings.HasPrefix(taskID, "////")
	}
	return strings.HasPrefix(hexID, DriverTaskIDPrefix)
}

// lineageBuilder encapsulates the state needed to build a lineage tree.
type lineageBuilder struct {
	// Input data
	tasksByID  map[string]eventtypes.Task
	actorsByID map[string]eventtypes.Actor

	// Indexes
	actorCreationTaskID map[string]string // actorID -> creation taskID

	// Output
	taskGroupByID map[string]*NestedTaskSummary // taskID or "actor:{id}" -> group
	rootSummary   []*NestedTaskSummary

	// Counters
	totalTasks          int
	totalActorTasks     int
	totalActorScheduled int
}

// BuildLineageSummary constructs a hierarchical task summary following Ray's lineage algorithm.
// The algorithm has 5 steps:
//  1. Index all tasks by ID and track actor creation tasks
//  2. Build tree structure based on task ownership (actor tasks → actor node, others → parent task)
//  3. Merge siblings with the same name into GROUP nodes
//  4. Sort by running > pending > failed > timestamp
//  5. Calculate total state_counts by summing children recursively
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

// indexData indexes all tasks and actors for quick lookup.
// Also tracks actor creation tasks to determine actor ownership.
func (b *lineageBuilder) indexData(tasks []eventtypes.Task, actors []eventtypes.Actor) {
	for _, task := range tasks {
		b.tasksByID[task.TaskID] = task
		if task.Type == eventtypes.ACTOR_CREATION_TASK {
			b.actorCreationTaskID[task.ActorID] = task.TaskID
		}
	}
	for _, actor := range actors {
		b.actorsByID[actor.ActorID] = actor
	}
}

// buildTree iterates through all tasks and builds the tree structure.
func (b *lineageBuilder) buildTree(tasks []eventtypes.Task) {
	for _, task := range tasks {
		// Skip DRIVER_TASK - Ray's live API doesn't show them in lineage
		if task.Type == eventtypes.DRIVER_TASK {
			continue
		}

		group := b.getOrCreateTaskGroup(task.TaskID)
		if group == nil {
			continue
		}

		// Update state counts
		state := string(task.State)
		if state == "" {
			state = "UNKNOWN"
		}
		group.StateCounts[state]++

		// Update counters
		switch task.Type {
		case eventtypes.NORMAL_TASK:
			b.totalTasks++
		case eventtypes.ACTOR_CREATION_TASK:
			b.totalActorScheduled++
		case eventtypes.ACTOR_TASK:
			b.totalActorTasks++
		}
	}
}

// getOrCreateTaskGroup returns existing or creates new NestedTaskSummary for a task.
// Returns nil if the task or its parent chain is missing data.
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

	// Use name first, fallback to func_or_class_name
	name := task.Name
	if name == "" {
		name = task.FuncOrClassName
	}

	var timestamp *int64
	if !task.StartTime.IsZero() {
		ts := task.StartTime.UnixMilli()
		timestamp = &ts
	}

	group := &NestedTaskSummary{
		Name:        name,
		Key:         taskID,
		Type:        string(task.Type),
		Timestamp:   timestamp,
		StateCounts: make(map[string]int),
		Children:    make([]*NestedTaskSummary, 0),
		Link:        &Link{Type: "task", ID: taskID},
	}
	b.taskGroupByID[taskID] = group

	// Determine parent based on task type
	if task.Type == eventtypes.ACTOR_TASK || task.Type == eventtypes.ACTOR_CREATION_TASK {
		// Actor-related tasks: parent is the ACTOR node
		parent := b.getOrCreateActorGroup(task.ActorID)
		if parent != nil {
			parent.Children = append(parent.Children, group)
		}
	} else {
		// Normal tasks: check parent_task_id
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

// getOrCreateActorGroup returns existing or creates new ACTOR node.
// The ACTOR node's parent is determined by the creation task's parent.
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

	// Fallback: extract from creation task's func name (e.g., "Counter.__init__" -> "Counter")
	if actorName == "" {
		if creationTaskID, ok := b.actorCreationTaskID[actorID]; ok {
			if creationTask, ok := b.tasksByID[creationTaskID]; ok {
				parts := strings.Split(creationTask.FuncOrClassName, ".")
				actorName = parts[0]
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
			if !creationTask.StartTime.IsZero() {
				ts := creationTask.StartTime.UnixMilli()
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

	// Determine ACTOR node's parent (same as creation task's parent)
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

// mergeSiblings groups children with the same name.
// If multiple children share a name, they are wrapped in a GROUP node.
// Single children are kept as-is.
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

// calculateTotals recursively sums children's state_counts into parent.
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

// sortGroups sorts by: running > pending > failed > timestamp > actor_creation first
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

// countRunning returns the total count of running states
func countRunning(g *NestedTaskSummary) int {
	return g.StateCounts["RUNNING"] +
		g.StateCounts["RUNNING_IN_RAY_GET"] +
		g.StateCounts["RUNNING_IN_RAY_WAIT"]
}

// countPending returns the total count of pending states
func countPending(g *NestedTaskSummary) int {
	return g.StateCounts["PENDING_ARGS_AVAIL"] +
		g.StateCounts["PENDING_NODE_ASSIGNMENT"] +
		g.StateCounts["PENDING_OBJ_STORE_MEM_AVAIL"] +
		g.StateCounts["PENDING_ARGS_FETCH"]
}

// getTimestamp returns the timestamp or MaxInt64 if nil
func getTimestamp(g *NestedTaskSummary) int64 {
	if g.Timestamp == nil {
		return math.MaxInt64
	}
	return *g.Timestamp
}

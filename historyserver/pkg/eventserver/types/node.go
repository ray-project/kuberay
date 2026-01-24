package types

import (
	"sync"
	"time"
)

// NodeState is the state of a node.
type NodeState string

// TODO(jwj): Handle redeclaration of states with the actor type.
// actor.go also defines ALIVE and DEAD.
const (
	NODE_ALIVE NodeState = "ALIVE"
	NODE_DEAD  NodeState = "DEAD"
)

// NodeAliveSubState provides more granular state information for nodes in the ALIVE state.
type NodeAliveSubState string

const (
	UNSPECIFIED NodeAliveSubState = "UNSPECIFIED"
	DRAINING    NodeAliveSubState = "DRAINING"
)

// NodeDeathInfoReason specifies the reason why a node died.
type NodeDeathInfoReason string

const (
	DEATH_REASON_UNSPECIFIED   NodeDeathInfoReason = "UNSPECIFIED"
	EXPECTED_TERMINATION       NodeDeathInfoReason = "EXPECTED_TERMINATION"
	UNEXPECTED_TERMINATION     NodeDeathInfoReason = "UNEXPECTED_TERMINATION"
	AUTOSCALER_DRAIN_PREEMPTED NodeDeathInfoReason = "AUTOSCALER_DRAIN_PREEMPTED"
	AUTOSCALER_DRAIN_IDLE      NodeDeathInfoReason = "AUTOSCALER_DRAIN_IDLE"
)

type NodeDeathInfo struct {
	Reason        NodeDeathInfoReason `json:"reason"`
	ReasonMessage string              `json:"reasonMessage"`
}

// NodeStateTransition represents a change in a node's state at a specific timestamp.
type NodeStateTransition struct {
	// State of a node (ALIVE, DEAD).
	State NodeState `json:"state"`

	// Timestamp when the state transition occurred.
	Timestamp time.Time `json:"timestamp"`

	// Resources available on a node (cpu, gpu, etc.), available only in the ALIVE state.
	// Resources map[string]float64 `json:"resources,omitempty"`

	// Reason why a node died (UNSPECIFIED, EXPECTED_TERMINATION, UNEXPECTED_TERMINATION, AUTOSCALER_DRAIN_PREEMPTED, AUTOSCALER_DRAIN_IDLE),
	// available only in the DEAD state
	// DeathInfo *NodeDeathInfo `json:"deathInfo,omitempty"`

	// Sub-state of a node in the ALIVE state (UNSPECIFIED, DRAINING), available only in the ALIVE state.
	// AliveSubState NodeAliveSubState `json:"aliveSubState,omitempty"`
}

type Node struct {
	NodeID           string                `json:"nodeId"`
	StateTransitions []NodeStateTransition `json:"stateTransitions"`
}

type NodeMap struct {
	NodeMap map[string]Node
	Mu      sync.Mutex
}

func (n *NodeMap) Lock() {
	n.Mu.Lock()
}

func (n *NodeMap) Unlock() {
	n.Mu.Unlock()
}

func NewNodeMap() *NodeMap {
	return &NodeMap{
		NodeMap: make(map[string]Node),
	}
}

type ClusterNodeMap struct {
	ClusterNodeMap map[string]*NodeMap
	Mu             sync.RWMutex
}

func (c *ClusterNodeMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterNodeMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterNodeMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterNodeMap) Unlock() {
	c.Mu.Unlock()
}

func (c *ClusterNodeMap) GetOrCreateNodeMap(clusterName string) *NodeMap {
	c.Lock()
	defer c.Unlock()

	nodeMap, exists := c.ClusterNodeMap[clusterName]
	if !exists {
		nodeMap = NewNodeMap()
		c.ClusterNodeMap[clusterName] = nodeMap
	}
	return nodeMap
}

func (n *NodeMap) CreateOrMergeNode(nodeId string, mergeFn func(*Node)) {
	n.Lock()
	defer n.Unlock()

	node, exists := n.NodeMap[nodeId]
	if !exists {
		node = Node{NodeID: nodeId}
	}

	mergeFn(&node)
	n.NodeMap[nodeId] = node
}

func (n Node) DeepCopy() Node {
	cp := n
	if len(n.StateTransitions) > 0 {
		cp.StateTransitions = make([]NodeStateTransition, len(n.StateTransitions))
		copy(cp.StateTransitions, n.StateTransitions)
	}
	return cp
}

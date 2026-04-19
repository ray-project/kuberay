package types

import (
	"sync"
	"time"
)

// NodeState is the state of a node.
type NodeState string

const (
	NODE_ALIVE NodeState = "ALIVE"
	NODE_DEAD  NodeState = "DEAD"
)

// NodeDeathInfoReason specifies the reason why a node died.
type NodeDeathInfoReason string

const (
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
	Resources map[string]float64 `json:"resources,omitempty"`

	// Reason why a node died (EXPECTED_TERMINATION, UNEXPECTED_TERMINATION, AUTOSCALER_DRAIN_PREEMPTED, AUTOSCALER_DRAIN_IDLE),
	// available only in the DEAD state.
	DeathInfo *NodeDeathInfo `json:"deathInfo,omitempty"`
}

// GetState returns the state of the node.
func (n NodeStateTransition) GetState() string {
	return string(n.State)
}

// GetTimestamp returns the timestamp of the node state transition.
func (n NodeStateTransition) GetTimestamp() time.Time {
	return n.Timestamp
}

// Node represents a Ray node in a cluster session. The fields are populated from both
// the NODE_DEFINITION_EVENT and the NODE_LIFECYCLE_EVENT.
type Node struct {
	// NodeID is the hexadecimal representation of the node ID.
	NodeID         string            `json:"nodeId"`
	NodeIPAddress  string            `json:"nodeIpAddress"`
	StartTimestamp time.Time         `json:"startTimestamp"`
	Labels         map[string]string `json:"labels"`

	// Wait for Ray to export the following fields.
	// Ref: https://github.com/ray-project/ray/issues/60129
	Hostname         string `json:"hostname,omitempty"`
	NodeName         string `json:"nodeName,omitempty"`
	InstanceID       string `json:"instanceId,omitempty"`
	InstanceTypeName string `json:"instanceTypeName,omitempty"`

	// StateTransitions contains the history of node state changes.
	// This field is populated only when at least one NODE_LIFECYCLE_EVENT has been processed.
	// Each transition includes the state (ALIVE/DEAD), timestamp, and state-specific data
	// (Resources for ALIVE, DeathInfo for DEAD).
	StateTransitions []NodeStateTransition `json:"stateTransitions,omitempty"`
}

type NodeMap struct {
	nodeMap map[string]Node
	mu      sync.Mutex
}

func NewNodeMap() *NodeMap {
	return &NodeMap{
		nodeMap: make(map[string]Node),
	}
}

type ClusterNodeMap struct {
	// ClusterNodeMap is a map of cluster session ID to NodeMap.
	clusterNodeMap map[string]*NodeMap
	mu             sync.RWMutex
}

func NewClusterNodeMap() *ClusterNodeMap {
	return &ClusterNodeMap{
		clusterNodeMap: make(map[string]*NodeMap),
	}
}

// GetOrCreateNodeMap retrieves the NodeMap for the given cluster session, creating it if it doesn't exist.
func (c *ClusterNodeMap) GetOrCreateNodeMap(clusterSessionID string) *NodeMap {
	c.mu.Lock()
	defer c.mu.Unlock()

	nodeMap, exists := c.clusterNodeMap[clusterSessionID]
	if !exists {
		nodeMap = NewNodeMap()
		c.clusterNodeMap[clusterSessionID] = nodeMap
	}
	return nodeMap
}

// GetNodeMap returns all nodes for a cluster session as deep copies.
func (c *ClusterNodeMap) GetNodeMap(clusterSessionID string) map[string]Node {
	c.mu.RLock()
	nodeMap, ok := c.clusterNodeMap[clusterSessionID]
	c.mu.RUnlock()
	if !ok {
		return map[string]Node{}
	}

	return nodeMap.GetNodeMap()
}

// GetNodeByNodeID returns a node by ID in a cluster session as a deep copy.
func (c *ClusterNodeMap) GetNodeByNodeID(clusterSessionID, nodeID string) (Node, bool) {
	c.mu.RLock()
	nodeMap, ok := c.clusterNodeMap[clusterSessionID]
	c.mu.RUnlock()
	if !ok {
		return Node{}, false
	}

	return nodeMap.GetNodeByNodeID(nodeID)
}

// CreateOrMergeNode retrieves or creates a Node and applies the merge function.
func (n *NodeMap) CreateOrMergeNode(nodeId string, mergeFn func(*Node)) {
	n.mu.Lock()
	defer n.mu.Unlock()

	node, exists := n.nodeMap[nodeId]
	if !exists {
		node = Node{NodeID: nodeId}
	}

	mergeFn(&node)
	n.nodeMap[nodeId] = node
}

// GetNodeMap returns all nodes as deep copies keyed by node ID.
func (n *NodeMap) GetNodeMap() map[string]Node {
	n.mu.Lock()
	defer n.mu.Unlock()

	nodes := make(map[string]Node, len(n.nodeMap))
	for id, node := range n.nodeMap {
		nodes[id] = node.DeepCopy()
	}

	return nodes
}

// GetNodeByNodeID returns a node by ID as a deep copy.
func (n *NodeMap) GetNodeByNodeID(nodeID string) (Node, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	node, ok := n.nodeMap[nodeID]
	if !ok {
		return Node{}, false
	}

	return node.DeepCopy(), true
}

// DeepCopy returns a deep copy of the Node, which prevents race conditions.
func (n Node) DeepCopy() Node {
	cp := n

	if len(n.Labels) > 0 {
		cp.Labels = make(map[string]string, len(n.Labels))
		for k, v := range n.Labels {
			cp.Labels[k] = v
		}
	}

	if len(n.StateTransitions) > 0 {
		cp.StateTransitions = make([]NodeStateTransition, len(n.StateTransitions))
		for i, tr := range n.StateTransitions {
			cp.StateTransitions[i] = tr

			if len(tr.Resources) > 0 {
				cp.StateTransitions[i].Resources = make(map[string]float64, len(tr.Resources))
				for k, v := range tr.Resources {
					cp.StateTransitions[i].Resources[k] = v
				}
			}

			if tr.DeathInfo != nil {
				cp.StateTransitions[i].DeathInfo = &NodeDeathInfo{
					Reason:        tr.DeathInfo.Reason,
					ReasonMessage: tr.DeathInfo.ReasonMessage,
				}
			}
		}
	}

	return cp
}

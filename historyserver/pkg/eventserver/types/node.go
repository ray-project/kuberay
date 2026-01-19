package types

import (
	"maps"
	"sync"
	"time"
)

type NodeState string

const (
	NODE_ALIVE NodeState = "ALIVE"
	NODE_DEAD  NodeState = "DEAD"
)

// Node represents a Ray node in the cluster.
// Populated from NODE_DEFINITION_EVENT and NODE_LIFECYCLE_EVENT.
type Node struct {
	// NodeID is from labels["ray.io/node-id"])
	NodeID string `json:"nodeId"`

	NodeIPAddress string `json:"nodeIpAddress"`

	// NodeGroup is from labels["ray.io/node-group"]) e.g., "headgroup", "cpu", "gpu"
	NodeGroup string `json:"nodeGroup"`

	// State is the current node state (ALIVE or DEAD)
	State NodeState `json:"state"`

	// Resources contains the node's total resources (CPU, memory, etc.)
	// Populated from the ALIVE state transition
	Resources map[string]float64 `json:"resources,omitempty"`

	// Labels contains all node labels from the definition event
	Labels map[string]string `json:"labels,omitempty"`

	// IsHead indicates if this is the head node (has node:__internal_head__ resource)
	IsHead bool `json:"isHead"`

	// StartTimestamp is when the node started
	StartTimestamp time.Time `json:"startTimestamp"`

	// DeadTimestamp is when the node died (if State == DEAD)
	DeadTimestamp time.Time `json:"deadTimestamp"`
}

// NodeMap uses NodeID as key
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

// ClusterNodeMap uses the cluster name as the key
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

// GetOrCreateNodeMap returns the NodeMap for the given cluster, creating it if it doesn't exist.
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

// CreateOrMergeNode finds or creates a node and applies the merge function.
func (n *NodeMap) CreateOrMergeNode(nodeId string, mergeFn func(*Node)) {
	n.Lock()
	defer n.Unlock()

	node, exists := n.NodeMap[nodeId]
	if !exists {
		newNode := Node{NodeID: nodeId}
		mergeFn(&newNode)
		n.NodeMap[nodeId] = newNode
		return
	}

	mergeFn(&node)
	n.NodeMap[nodeId] = node
}

// DeepCopy returns a deep copy of the Node.
func (n Node) DeepCopy() Node {
	cp := n
	if len(n.Resources) > 0 {
		cp.Resources = make(map[string]float64, len(n.Resources))
		maps.Copy(cp.Resources, n.Resources)
	}
	if len(n.Labels) > 0 {
		cp.Labels = make(map[string]string, len(n.Labels))
		for k, v := range n.Labels {
			cp.Labels[k] = v
		}
	}
	return cp
}

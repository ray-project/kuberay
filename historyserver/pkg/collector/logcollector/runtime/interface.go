package runtime

type RayLogCollector interface {
	Run(stop <-chan struct{}) error
	UpdateNodeID(nodeID string)
	HandleSessionChange(newSessionDir string) error
}

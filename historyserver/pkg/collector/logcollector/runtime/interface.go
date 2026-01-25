package runtime

type RayLogCollector interface {
	Start(stop <-chan struct{}) error
	Run(stop <-chan struct{}) error
	WaitForStop() <-chan struct{}
}

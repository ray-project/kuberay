package runtime

type RayLogCollector interface {
	Start(chan struct{})
}

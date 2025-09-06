package runtime

type RayLogCollector interface {
	Start(<-chan int)
}

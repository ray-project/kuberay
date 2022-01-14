package util

// ClientOptions contains configuration needed to create a Kubernetes client
type ClientOptions struct {
	QPS   float32
	Burst int
}

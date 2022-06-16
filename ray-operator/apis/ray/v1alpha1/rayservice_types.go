package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ServiceStatus string

const (
	FailToGetOrCreateRayCluster  ServiceStatus = "FailToGetOrCreateRayCluster"
	WaitForDashboard             ServiceStatus = "WaitForDashboard"
	FailServeDeploy              ServiceStatus = "FailServeDeploy"
	FailGetServeDeploymentStatus ServiceStatus = "FailGetServeDeploymentStatus"
	Running                      ServiceStatus = "Running"
	Restarting                   ServiceStatus = "Restarting"
	FailDeleteRayCluster         ServiceStatus = "FailDeleteRayCluster"
	FailUpdateIngress            ServiceStatus = "FailUpdateIngress"
	FailUpdateService            ServiceStatus = "FailUpdateService"
)

// RayServiceSpec defines the desired state of RayService
type RayServiceSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	ServeConfigSpecs []ServeConfigSpec `json:"serveConfigs,omitempty"`
	RayClusterSpec   RayClusterSpec    `json:"rayClusterConfig,omitempty"`
}

// ServeConfigSpec defines the desired state of RayService
// Reference to https://docs.ray.io/en/latest/ray-core/package-ref.html#ray-remote.
type ServeConfigSpec struct {
	Name                      string             `json:"name"`
	ImportPath                string             `json:"importPath"`
	InitArgs                  []string           `json:"initArgs,omitempty"`
	InitKwargs                map[string]string  `json:"initKwargs,omitempty"`
	NumReplicas               *int32             `json:"numReplicas,omitempty"`
	RoutePrefix               string             `json:"routePrefix,omitempty"`
	MaxConcurrentQueries      *int32             `json:"maxConcurrentQueries,omitempty"`
	UserConfig                map[string]string  `json:"userConfig,omitempty"`
	AutoscalingConfig         map[string]string  `json:"autoscalingConfig,omitempty"`
	GracefulShutdownWaitLoopS *int32             `json:"gracefulShutdownWaitLoopS,omitempty"`
	GracefulShutdownTimeoutS  *int32             `json:"gracefulShutdownTimeoutS,omitempty"`
	HealthCheckPeriodS        *int32             `json:"healthCheckPeriodS,omitempty"`
	HealthCheckTimeoutS       *int32             `json:"healthCheckTimeoutS,omitempty"`
	RayActorOptions           RayActorOptionSpec `json:"rayActorOptions,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor
type RayActorOptionSpec struct {
	RuntimeEnv        map[string][]string `json:"runtimeEnv,omitempty"`
	NumCpus           *float64            `json:"numCpus,omitempty"`
	NumGpus           *float64            `json:"numGpus,omitempty"`
	Memory            *int32              `json:"memory,omitempty"`
	ObjectStoreMemory *int32              `json:"objectStoreMemory,omitempty"`
	Resources         map[string]string   `json:"resources,omitempty"`
	AcceleratorType   string              `json:"acceleratorType,omitempty"`
}

// RayServiceStatus defines the observed state of RayService
type RayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	ServiceStatus        ServiceStatus           `json:"serviceStatus,omitempty"`
	ServeStatuses        []ServeDeploymentStatus `json:"serveDeploymentStatuses,omitempty"`
	DashboardStatus      DashboardStatus         `json:"dashboardStatus,omitempty"`
	ActiveRayClusterName string                  `json:"activeRayClusterName,omitempty"`
	// Pending RayCluster Name indicates a RayCluster will be created or is under creating.
	PendingRayClusterName string           `json:"pendingRayClusterName,omitempty"`
	RayClusterStatus      RayClusterStatus `json:"rayClusterStatus,omitempty"`
}

// DashboardStatus defines the current states of Ray Dashboard
type DashboardStatus struct {
	IsHealthy      bool         `json:"isHealthy,omitempty"`
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// Keep track of how long the service is healthy.
	// Update when Serve Deployment is healthy or first time convert to unhealthy from healthy.
	HealthLastUpdateTime *metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

// ServeDeploymentStatuses defines the current states of all Serve Deployments
type ServeDeploymentStatuses struct {
	Statuses []ServeDeploymentStatus `json:"statuses,omitempty"`
}

// ServeDeploymentStatus defines the current state of Serve Deployment
type ServeDeploymentStatus struct {
	// Name, Status, Message are from Ray Dashboard to represent the state of a serve deployment.
	Name string `json:"name,omitempty"`
	// TODO: change status type to enum
	Status         string       `json:"status,omitempty"`
	Message        string       `json:"message,omitempty"`
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// Keep track of how long the service is healthy.
	// Update when Serve Deployment is healthy or first time convert to unhealthy from healthy.
	HealthLastUpdateTime *metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RayService is the Schema for the rayservices API
type RayService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RayServiceSpec   `json:"spec,omitempty"`
	Status RayServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RayServiceList contains a list of RayService
type RayServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayService{}, &RayServiceList{})
}

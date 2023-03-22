package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ServiceStatus string

const (
	FailedToGetOrCreateRayCluster    ServiceStatus = "FailedToGetOrCreateRayCluster"
	WaitForDashboard                 ServiceStatus = "WaitForDashboard"
	WaitForServeDeploymentReady      ServiceStatus = "WaitForServeDeploymentReady"
	FailedToGetServeDeploymentStatus ServiceStatus = "FailedToGetServeDeploymentStatus"
	Running                          ServiceStatus = "Running"
	Restarting                       ServiceStatus = "Restarting"
	FailedToUpdateIngress            ServiceStatus = "FailedToUpdateIngress"
	FailedToUpdateServingPodLabel    ServiceStatus = "FailedToUpdateServingPodLabel"
	FailedToUpdateService            ServiceStatus = "FailedToUpdateService"
)

// These statuses should match Ray Serve's application statuses
var ApplicationStatusEnum = struct {
	NOT_STARTED   string
	DEPLOYING     string
	RUNNING       string
	DEPLOY_FAILED string
}{
	NOT_STARTED:   "NOT_STARTED",
	DEPLOYING:     "DEPLOYING",
	RUNNING:       "RUNNING",
	DEPLOY_FAILED: "DEPLOY_FAILED",
}

// These statuses should match Ray Serve's deployment statuses
var DeploymentStatusEnum = struct {
	UPDATING  string
	HEALTHY   string
	UNHEALTHY string
}{
	UPDATING:  "UPDATING",
	HEALTHY:   "HEALTHY",
	UNHEALTHY: "UNHEALTHY",
}

// RayServiceSpec defines the desired state of RayService
type RayServiceSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	ServeDeploymentGraphSpec           ServeDeploymentGraphSpec `json:"serveConfig,omitempty"`
	RayClusterSpec                     RayClusterSpec           `json:"rayClusterConfig,omitempty"`
	ServiceUnhealthySecondThreshold    *int32                   `json:"serviceUnhealthySecondThreshold,omitempty"`
	DeploymentUnhealthySecondThreshold *int32                   `json:"deploymentUnhealthySecondThreshold,omitempty"`
}

type ServeDeploymentGraphSpec struct {
	ImportPath       string            `json:"importPath"`
	RuntimeEnv       string            `json:"runtimeEnv,omitempty"`
	ServeConfigSpecs []ServeConfigSpec `json:"deployments,omitempty"`
}

// ServeConfigSpec defines the desired state of RayService
// Reference to http://rayserve.org
type ServeConfigSpec struct {
	Name                      string             `json:"name"`
	NumReplicas               *int32             `json:"numReplicas,omitempty"`
	RoutePrefix               string             `json:"routePrefix,omitempty"`
	MaxConcurrentQueries      *int32             `json:"maxConcurrentQueries,omitempty"`
	UserConfig                string             `json:"userConfig,omitempty"`
	AutoscalingConfig         string             `json:"autoscalingConfig,omitempty"`
	GracefulShutdownWaitLoopS *int32             `json:"gracefulShutdownWaitLoopS,omitempty"`
	GracefulShutdownTimeoutS  *int32             `json:"gracefulShutdownTimeoutS,omitempty"`
	HealthCheckPeriodS        *int32             `json:"healthCheckPeriodS,omitempty"`
	HealthCheckTimeoutS       *int32             `json:"healthCheckTimeoutS,omitempty"`
	RayActorOptions           RayActorOptionSpec `json:"rayActorOptions,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor
type RayActorOptionSpec struct {
	RuntimeEnv        string   `json:"runtimeEnv,omitempty"`
	NumCpus           *float64 `json:"numCpus,omitempty"`
	NumGpus           *float64 `json:"numGpus,omitempty"`
	Memory            *int32   `json:"memory,omitempty"`
	ObjectStoreMemory *int32   `json:"objectStoreMemory,omitempty"`
	Resources         string   `json:"resources,omitempty"`
	AcceleratorType   string   `json:"acceleratorType,omitempty"`
}

// RayServiceStatuses defines the observed state of RayService
// +kubebuilder:printcolumn:name="ServiceStatus",type=string,JSONPath=".status.serviceStatus"
type RayServiceStatuses struct {
	ActiveServiceStatus RayServiceStatus `json:"activeServiceStatus,omitempty"`
	// Pending Service Status indicates a RayCluster will be created or is being created.
	PendingServiceStatus RayServiceStatus `json:"pendingServiceStatus,omitempty"`
	// ServiceStatus indicates the current RayService status.
	ServiceStatus ServiceStatus `json:"serviceStatus,omitempty"`
	// observedGeneration is the most recent generation observed for this RayService. It corresponds to the
	// RayService's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type RayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	ApplicationStatus AppStatus               `json:"appStatus,omitempty"`
	ServeStatuses     []ServeDeploymentStatus `json:"serveDeploymentStatuses,omitempty"`
	DashboardStatus   DashboardStatus         `json:"dashboardStatus,omitempty"`
	RayClusterName    string                  `json:"rayClusterName,omitempty"`
	RayClusterStatus  RayClusterStatus        `json:"rayClusterStatus,omitempty"`
}

// DashboardStatus defines the current states of Ray Dashboard
type DashboardStatus struct {
	IsHealthy      bool         `json:"isHealthy,omitempty"`
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// Keep track of how long the dashboard is healthy.
	// Update when Dashboard is responsive or first time convert to non-responsive from responsive.
	HealthLastUpdateTime *metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

type AppStatus struct {
	Status         string       `json:"status,omitempty"`
	Message        string       `json:"message,omitempty"`
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// Keep track of how long the service is healthy.
	// Update when Serve deployment is healthy or first time convert to unhealthy from healthy.
	HealthLastUpdateTime *metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

// ServeDeploymentStatus defines the current state of a Serve deployment
type ServeDeploymentStatus struct {
	// Name, Status, Message are from Ray Dashboard and represent a Serve deployment's state.
	Name string `json:"name,omitempty"`
	// TODO: change status type to enum
	Status         string       `json:"status,omitempty"`
	Message        string       `json:"message,omitempty"`
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
	// Keep track of how long the service is healthy.
	// Update when Serve deployment is healthy or first time convert to unhealthy from healthy.
	HealthLastUpdateTime *metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+genclient
// RayService is the Schema for the rayservices API
type RayService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RayServiceSpec     `json:"spec,omitempty"`
	Status RayServiceStatuses `json:"status,omitempty"`
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

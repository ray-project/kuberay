package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ServiceStatus string

const (
	FAIL_TO_GET_RAYSERVICE           ServiceStatus = "fail_to_get_rayservice"
	FAIL_TO_GET_OR_CREATE_RAYCLUSTER ServiceStatus = "fail_to_get_or_create_RAYCLUSTER"
	WAIT_FOR_DASHBOARD               ServiceStatus = "wait_for_dashboard"
	FAIL_SERVE_DEPLOY                ServiceStatus = "fail_serve_deploy"
	FAIL_GET_SERVE_DEPLOYMENT_STATUS ServiceStatus = "fail_get_serve_deployment_status"
	RUNNING                          ServiceStatus = "running"
	RESTARTING                       ServiceStatus = "restarting"
	FAIL_DELETE_RAYCLUSTER           ServiceStatus = "fail_delete_raycluster"
)

// ServeConfigSpec defines the desired state of RayService
type ServeConfigSpec struct {
	Name                      string             `json:"name"`
	ImportPath                string             `json:"import_path"`
	InitArgs                  []string           `json:"init_args,omitempty"`
	InitKwargs                map[string]string  `json:"init_kwargs,omitempty"`
	NumReplicas               *int32             `json:"num_replicas,omitempty"`
	RoutePrefix               string             `json:"route_prefix,omitempty"`
	MaxConcurrentQueries      *int32             `json:"max_concurrent_queries,omitempty"`
	UserConfig                map[string]string  `json:"user_config,omitempty"`
	AutoscalingConfig         map[string]string  `json:"autoscaling_config,omitempty"`
	GracefulShutdownWaitLoopS *float64           `json:"graceful_shutdown_wait_loop_s,omitempty"`
	GracefulShutdownTimeoutS  *float64           `json:"graceful_shutdown_timeout_s,omitempty"`
	HealthCheckPeriodS        *float64           `json:"health_check_period_s,omitempty"`
	HealthCheckTimeoutS       *float64           `json:"health_check_timeout_s,omitempty"`
	RayActorOptions           RayActorOptionSpec `json:"ray_actor_options,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor
type RayActorOptionSpec struct {
	RuntimeEnv        map[string][]string `json:"runtime_env,omitempty"`
	NumCpus           *float64            `json:"num_cpus,omitempty"`
	NumGpus           *float64            `json:"num_gpus,omitempty"`
	Memory            *int32              `json:"memory,omitempty"`
	ObjectStoreMemory *int32              `json:"object_store_memory,omitempty"`
	Resources         map[string]string   `json:"resources,omitempty"`
	AcceleratorType   string              `json:"accelerator_type,omitempty"`
}

// RayServiceSpec defines the desired state of RayService
type RayServiceSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	HealthCheckProbe *v1.Probe         `json:"healthCheckConfig,omitempty"`
	ServeConfigSpecs []ServeConfigSpec `json:"serveConfigs,omitempty"`
	RayClusterSpec   RayClusterSpec    `json:"rayClusterConfig,omitempty"`
}

// ServeStatus defines the desired state of Serve Deployment
type ServeStatus struct {
	Name    string `json:"name,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	// Keep track of how long the service is healthy.
	// Update when Serve Deployment is healthy or first time convert to unhealthy from healthy.
	HealthLastUpdateTime metav1.Time `json:"healthLastUpdateTime,omitempty"`
}

// ServeStatuses defines the desired states of all Serve Deployments
type ServeStatuses struct {
	Statuses []ServeStatus `json:"statuses,omitempty"`
}

// RayServiceStatus defines the observed state of RayService
type RayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	ServiceStatus    ServiceStatus    `json:"serviceStatus,omitempty"`
	ServeStatuses    ServeStatuses    `json:"serveStatuses,omitempty"`
	RayClusterStatus RayClusterStatus `json:"rayClusterStatus,omitempty"`
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

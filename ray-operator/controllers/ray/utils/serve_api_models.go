package utils

// Defines which Ray Serve config schema is used for `serveConfigV2`; can be SINGLE_APP or MULTI_APP. Please see the Ray Serve docs
// https://docs.ray.io/en/latest/serve/api/doc/ray.serve.schema.ServeApplicationSchema.html and
// https://docs.ray.io/en/latest/serve/api/doc/ray.serve.schema.ServeDeploySchema.html for the
// single-application schema and multi-application schema, respectively.
type RayServeConfigType string

const (
	MULTI_APP  RayServeConfigType = "MULTI_APP"
	SINGLE_APP RayServeConfigType = "SINGLE_APP"
)

// V1 Serve API Response format
type ServeAppStatusInfoV1 struct {
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type ServeSingleApplicationStatusV1 struct {
	ApplicationStatus  ServeAppStatusInfoV1    `json:"app_status,omitempty"`
	DeploymentStatuses []ServeDeploymentStatus `json:"deployment_statuses,omitempty"`
}

// ServeDeploymentStatus and ServeApplicationStatus describe the format of status(es) that will
// be returned by GetSingleApplicationStatus and GetMultiApplicationStatus methods of the dashboard client
// Describes the status of a deployment
type ServeDeploymentStatus struct {
	Name    string `json:"name,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// Describes the status of an application
type ServeApplicationStatus struct {
	Name        string                           `json:"name,omitempty"`
	Status      string                           `json:"status"`
	Message     string                           `json:"message,omitempty"`
	Deployments map[string]ServeDeploymentStatus `json:"deployments"`
}

// V2 Serve API Response format. These extend the ServeDeploymentStatus and ServeApplicationStatus structs,
// but contain more information such as route prefix because the V2/multi-app GET API fetchs general metadata,
// not just statuses.
type ServeDeploymentDetails struct {
	ServeDeploymentStatus
	RoutePrefix string `json:"route_prefix,omitempty"`
}

type ServeApplicationDetails struct {
	ServeApplicationStatus
	RoutePrefix string                            `json:"route_prefix,omitempty"`
	DocsPath    string                            `json:"docs_path,omitempty"`
	Deployments map[string]ServeDeploymentDetails `json:"deployments"`
}

type ServeDetails struct {
	Applications map[string]ServeApplicationDetails `json:"applications"`
	DeployMode   string                             `json:"deploy_mode,omitempty"`
}

// ServingClusterDeployments defines the request sent to the dashboard api server.
// See https://docs.ray.io/en/master/_modules/ray/serve/schema.html#ServeApplicationSchema for more details.
type ServingClusterDeployments struct {
	ImportPath  string                 `json:"import_path"`
	RuntimeEnv  map[string]interface{} `json:"runtime_env,omitempty"`
	Deployments []ServeConfigSpec      `json:"deployments,omitempty"`
	Port        int                    `json:"port,omitempty"`
}

// ServeConfigSpec defines the (single-application) desired state of RayService, used by Ray Dashboard.
// Serve schema details: https://docs.ray.io/en/latest/serve/api/doc/ray.serve.schema.ServeApplicationSchema.html
type ServeConfigSpec struct {
	Name                      string                 `json:"name"`
	NumReplicas               *int32                 `json:"num_replicas,omitempty"`
	RoutePrefix               string                 `json:"route_prefix,omitempty"`
	MaxConcurrentQueries      *int32                 `json:"max_concurrent_queries,omitempty"`
	UserConfig                map[string]interface{} `json:"user_config,omitempty"`
	AutoscalingConfig         map[string]interface{} `json:"autoscaling_config,omitempty"`
	GracefulShutdownWaitLoopS *int32                 `json:"graceful_shutdown_wait_loop_s,omitempty"`
	GracefulShutdownTimeoutS  *int32                 `json:"graceful_shutdown_timeout_s,omitempty"`
	HealthCheckPeriodS        *int32                 `json:"health_check_period_s,omitempty"`
	HealthCheckTimeoutS       *int32                 `json:"health_check_timeout_s,omitempty"`
	RayActorOptions           RayActorOptionSpec     `json:"ray_actor_options,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor, used by Ray Dashboard.
type RayActorOptionSpec struct {
	RuntimeEnv        map[string]interface{} `json:"runtime_env,omitempty"`
	NumCpus           *float64               `json:"num_cpus,omitempty"`
	NumGpus           *float64               `json:"num_gpus,omitempty"`
	Memory            *int32                 `json:"memory,omitempty"`
	ObjectStoreMemory *int32                 `json:"object_store_memory,omitempty"`
	Resources         map[string]interface{} `json:"resources,omitempty"`
	AcceleratorType   string                 `json:"accelerator_type,omitempty"`
}

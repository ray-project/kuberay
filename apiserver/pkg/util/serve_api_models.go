package util

// Please see the Ray Serve docs
// https://docs.ray.io/en/latest/serve/api/doc/ray.serve.schema.ServeDeploySchema.html for the
// multi-application schema.

// ServeDeploymentStatus and ServeApplicationStatus describe the format of status(es) that will
// be returned by the GetMultiApplicationStatus method of the dashboard client
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
	Name        string             `json:"name,omitempty"`
	Host        string             `json:"host,omitempty"`
	Port        int32              `json:"port,omitempty"`
	RoutePrefix string             `json:"route_prefix,omitempty"`
	ImportPath  string             `json:"import_path,omitempty"`
	Args        map[string]string  `json:"args,omitempty"`
	Runtime     map[string]string  `json:"runtime_env,omitempty"`
	Deployments []DeploymentSchema `json:"deployments,omitempty"`
}

type ServeApplicationDetails struct {
	Name          string                                  `json:"name,omitempty"`
	Status        string                                  `json:"status"`
	Message       string                                  `json:"message,omitempty"`
	RoutePrefix   string                                  `json:"route_prefix,omitempty"`
	DocsPath      string                                  `json:"docs_path,omitempty"`
	LastDeployed  float32                                 `json:"last_deployed_time_s,omitempty"`
	Deployments   map[string]DeploymentApplicationDetails `json:"deployments,omitempty"`
	Configuration ServeDeploymentDetails                  `json:"deployed_app_config,omitempty"`
}

type DeploymentApplicationDetails struct {
	Name          string           `json:"name,omitempty"`
	Status        string           `json:"status"`
	Message       string           `json:"message,omitempty"`
	Configuration DeploymentSchema `json:"deployment_config,omitempty"`
	Replicas      []Replica        `json:"replicas,omitempty"`
}

type Replica struct {
	NodeId    string  `json:"node_id,omitempty"`
	NodeIp    string  `json:"node_ip,omitempty"`
	ActorId   string  `json:"actor_id,omitempty"`
	ActorName string  `json:"actor_name,omitempty"`
	WorkerId  string  `json:"worker_id,omitempty"`
	State     string  `json:"state,omitempty"`
	LogFile   string  `json:"log_file_path,omitempty"`
	ReplicaId string  `json:"replica_id,omitempty"`
	Pid       int32   `json:"pid,omitempty"`
	StartTime float32 `json:"start_time_s,omitempty"`
}

type DeploymentSchema struct {
	Name                    string                 `json:"name,omitempty"`
	PLacementStrategy       string                 `json:"placement_group_strategy,omitempty"`
	PLacementBundles        []map[string]float32   `json:"placement_group_bundles,omitempty"`
	NumReplicas             int32                  `json:"num_replicas,omitempty"`
	MaxReplicasNode         int32                  `json:"max_replicas_per_node,omitempty"`
	MaxQueries              int32                  `json:"max_concurrent_queries,omitempty"`
	UserConfig              map[string]interface{} `json:"user_config,omitempty"`
	AutoScalingConfig       map[string]interface{} `json:"autoscaling_config,omitempty"`
	GracefulShutdownLoop    float32                `json:"graceful_shutdown_wait_loop_s,omitempty"`
	GracefulShutdownTimeout float32                `json:"graceful_shutdown_timeout_s,omitempty"`
	HealthCheckPeriod       float32                `json:"health_check_period_s,omitempty"`
	HealthCheckTmout        float32                `json:"health_check_timeout_s,omitempty"`
	ActorOptions            RayActorOptionSpec     `json:"ray_actor_options,omitempty"`
}

type ServeDetails struct {
	Applications   map[string]ServeApplicationDetails `json:"applications"`
	DeployMode     string                             `json:"deploy_mode,omitempty"`
	ProxyLocation  string                             `json:"proxy_location,omitempty"`
	ControllerInfo ControllerInfo                     `json:"controller_info,omitempty"`
	HTTPOptions    HTTPOptions                        `json:"http_options,omitempty"`
	GRPCOptions    GRPCOptions                        `json:"grpc_options,omitempty"`
	Proxies        map[string]Proxy                   `json:"proxies,omitempty"`
}

type ControllerInfo struct {
	NodeId      string `json:"node_id,omitempty"`
	NodeIp      string `json:"node_ip,omitempty"`
	ActorId     string `json:"actor_id,omitempty"`
	ActorName   string `json:"actor_name,omitempty"`
	WorkerId    string `json:"worker_id,omitempty"`
	LogFilePath string `json:"log_file_path,omitempty"`
}

type HTTPOptions struct {
	Host             string  `json:"host,omitempty"`
	Port             int32   `json:"port,omitempty"`
	RootPath         string  `json:"root_path,omitempty"`
	RequestTimeout   float32 `json:"request_timeout_s,omitempty"`
	KeepAliveTimeout int32   `json:"keep_alive_timeout_s,omitempty"`
}

type GRPCOptions struct {
	Port            int32    `json:"port,omitempty"`
	ServerFunctions []string `json:"grpc_servicer_functions,omitempty"`
}

type Proxy struct {
	NodeId    string `json:"node_id,omitempty"`
	NodeIp    string `json:"node_ip,omitempty"`
	ActorId   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name,omitempty"`
	WorkerId  string `json:"worker_id,omitempty"`
	Status    string `json:"status,omitempty"`
	LogFile   string `json:"log_file_path,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor, used by Ray Dashboard.
type RayActorOptionSpec struct {
	RuntimeEnv        map[string]interface{} `json:"runtime_env,omitempty"`
	NumCpus           *float64               `json:"num_cpus,omitempty"`
	NumGpus           *float64               `json:"num_gpus,omitempty"`
	Memory            *uint64                `json:"memory,omitempty"`
	ObjectStoreMemory *uint64                `json:"object_store_memory,omitempty"`
	Resources         map[string]interface{} `json:"resources,omitempty"`
	AcceleratorType   string                 `json:"accelerator_type,omitempty"`
}
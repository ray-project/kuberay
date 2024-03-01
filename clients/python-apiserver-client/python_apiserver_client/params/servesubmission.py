class ControllerInfo:
    """
    ControllerInfo used to define information about the Serve controller in a Ray cluster
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create ControllerInfo: gets the following parameters:
        nodeId - controller node id
        nodeIp - controller node IP
        actorId - controller actor id
        actorName - controller actor name
        workerId - controller worker id
        logFilePath - controller log file path
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.nodeId = dst.get("nodeId", "")
        self.nodeIp = dst.get("nodeIp", "")
        self.actorId = dst.get("actorId", "")
        self.actorName = dst.get("actorName", "")
        self.workerId = dst.get("workerId", None)
        self.logFilePath = dst.get("logFilePath", "")

    def to_string(self) -> str:
        val = f"node: ID = {self.nodeId}, IP = {self.nodeIp}; actor: ID = {self.actorId}, name = {self.actorName}"
        if self.workerId is not None:
            val += f"; worker = {self.workerId}"
        val += f"; log file = {self.logFilePath}"
        return val


class HTTPOptions:
    """
    HTTPOptions defines Options used to start the HTTP Proxy for Serve controller in a Ray cluster
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create HTTPOptions: gets the following parameters:
        host - host for HTTP servers to listen on.
        port - port for HTTP server.
        keepAliveTimeoutS - the HTTP proxy will keep idle connections alive for this duration before closing them when no requests are ongoing.
        requestTimeoutS - the timeout for HTTP requests.
        rootPath - root path to mount the serve application.
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.host = dst.get("host", "")
        self.port = dst.get("port", 0)
        self.keepAliveTimeout = dst.get("keepAliveTimeoutS", 0)
        self.requestTimeout = dst.get("requestTimeoutS", .0)
        self.rootPath = dst.get("rootPath", "")

    def to_string(self) -> str:
        val = f"host = {self.host}, port = {self.port}, keep alive timeout (s) = {self.keepAliveTimeout}"
        if self.requestTimeout > .1:
            val += f", request timeout (s) = {self.requestTimeout}"
        if self.rootPath != "":
            val += f", root path = {self.rootPath}"
        return val


class GRPCOptions:
    """
    GRPCOptions defines Options used to start the gRPC Proxy for Serve controller in a Ray cluster
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create GRPCOptions: gets the following parameters:
        port - port for gRPC server.
        grpcServicerFunctions - list of import paths for gRPC add_servicer_to_server functions to add to Serve’s gRPC proxy
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.port = dst.get("port", 0)
        self.servicerFunctions = dst.get("grpcServicerFunctions", [])

    def to_string(self) -> str:
        val = f"port = {self.port}"
        if len(self.servicerFunctions) > 0:
            val += f", server functions = {','.join(self.servicerFunctions)}"
        return val


class Proxy:
    """
    Proxy defines details about the Proxy
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create Proxy: gets the following parameters:
        nodeId - proxy node id
        nodeIp - proxy node IP
        actorId - proxy actor id
        actorName - proxy actor name
        workerId - proxy worker id
        status - proxy status
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.nodeId = dst.get("nodeId", "")
        self.nodeIp = dst.get("nodeIp", "")
        self.actorId = dst.get("actorId", "")
        self.actorName = dst.get("actorName", "")
        self.workerId = dst.get("workerId", "")
        self.status = dst.get("status", "")

    def to_string(self) -> str:
        return (f"node: ID = {self.nodeId}, IP = {self.nodeIp}; actor: ID = {self.actorId}, name = {self.actorName}; "
                f"worker = {self.workerId}; status = {self.status}")


class Replica:
    """
    Replica defines details about the live replicas of deployment
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create Replica: gets the following parameters:
        nodeId - replica node id
        nodeIp - replica node IP
        actorId - replica actor id
        actorName - replica actor name
        workerId - replica worker id
        state - replica state
        logFilePath - replica log location
        replicaId - replica id
        pid - replica process id
        startTimeS - replica start time
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.nodeId = dst.get("nodeId", "")
        self.nodeIp = dst.get("nodeIp", "")
        self.actorId = dst.get("actorId", "")
        self.actorName = dst.get("actorName", "")
        self.workerId = dst.get("workerId", "")
        self.state = dst.get("state", "")
        self.logFile = dst.get("logFilePath", "")
        self.replicaId = dst.get("replicaId", "")
        self.pid = dst.get("pid", 0)
        self.startTime = int(dst.get("startTimeS", 0))

    def to_string(self) -> str:
        return (f"node: ID = {self.nodeId}, IP = {self.nodeIp}; actor: ID = {self.actorId}, name = {self.actorName}; "
                f"worker = {self.workerId}; state = {self.state}; log file = {self.logFile}; pid = {self.pid}; start "
                f"time (s) = {self.startTime}")


class RayActorOptions:
    """
    RayActorOptions defines options set for each replica actor.
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create RayActorOptions: gets the following parameters:
          runtimeEnv - actor runtime environment
          numCpus - actor number of CPUs
          numGpus - actor number of GPUs
          memory - actor memory
          objectStoreMemory - actor object store memory
          resources - actor custom resources
          acceleratorType - actor accelerator (GPU) type
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.runtimeEnv = dst.get("runtimeEnv", {})
        self.cpus = dst.get("numCpus", .0)
        self.gpus = dst.get("numGpus", .0)
        self.memory = dst.get("memory", .0)
        self.objectMemory = dst.get("objectStoreMemory", .0)
        self.resources = dst.get("resources", {})
        self.accelerator = dst.get("acceleratorType", "")

    def to_string(self) -> str:
        empty = True
        val = ""
        if len(self.runtimeEnv) > 0:
            val += f"runtime env = {str(self.runtimeEnv)}"
            empty = False
        if self.cpus > .001:
            if not empty:
                val += " ,"
            val += f"cpu = {self.cpus}"
            empty = False
        if self.gpus > .001:
            if not empty:
                val += " ,"
            val += f"gpu = {self.gpus}"
            empty = False
        if self.memory > .001:
            if not empty:
                val += " ,"
            val += f"memory = {self.memory}"
            empty = False
        if self.objectMemory > .001:
            if not empty:
                val += " ,"
            val += f"object store memory = {self.objectMemory}"
            empty = False
        if len(self.resources) > 0:
            if not empty:
                val += " ,"
            val += f"resources = {str(self.resources)}"
            empty = False
        if self.accelerator != "":
            if not empty:
                val += " ,"
            val += f"gpu accelerator = {self.accelerator}"
        return val


class DeploymentSchema:
    """
    DeploymentSchema defines options for one deployment within a Serve application.
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create DeploymentSchema: gets the following parameters:
          name - globally-unique name identifying this deployment
          placementGroupStrategy - strategy to use for the replica placement groups
          placementGroupBundles - set of placement group bundles to be scheduled for each replica of this deployment
          numReplicas - number of processes that handle requests to this deployment
          maxReplicasPerNode - max number of deployment replicas can run on a single node
          maxConcurrentQueries - the max number of pending queries in a single replica
          userConfig - config to pass into this deployment’s reconfigure method.
          autoscalingConfig - specifying autoscaling parameters for the deployment’s number of replicas
          gracefulShutdownWaitLoopS - timeout until there is no more work to be done before shutting down.
          gracefulShutdownTimeoutS - timeout before forcefully killing the replica for shutdown
          healthCheckPeriodS - frequency at which the controller will health check replicas
          healthCheckTimeoutS - timeout that the controller will wait for a response from the replica’s health check before marking it unhealthy.
          rayActorOptions - options set for each replica actor
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.name = dst.get("name", "")
        self.placement_group_strategy = dst.get("placementGroupStrategy", "")
        self.placement_group_bundles = dst.get("placementGroupBundles", [])
        self.replicas = dst.get("numReplicas", 0)
        self.replicas_node = dst.get("maxReplicasPerNode", 0)
        self.queries = dst.get("maxConcurrentQueries", 0)
        self.user_config = dst.get("userConfig", {})
        self.autoscaling_config = dst.get("autoscalingConfig", {})
        self.graceful_shutdown_timeout = dst.get("gracefulShutdownTimeoutS", 0)
        self.graceful_shutdown_wait = dst.get("gracefulShutdownWaitLoopS", 0)
        self.health_period = dst.get("healthCheckPeriodS", 0)
        self.health_timeout = dst.get("healthCheckTimeoutS", 0)
        actor = dst.get("rayActorOptions", {})
        if (len(actor)) > 0:
            self.actor = RayActorOptions(actor)
        else:
            self.actor = None

    def to_string(self) -> str:
        val = (f"name = {self.name}, replicas = {self.replicas}, graceful shutdown timeout (s) = "
               f"{self.graceful_shutdown_timeout}, graceful shutdown wait (s) = {self.graceful_shutdown_wait}, health "
               f"check period (s) = {self.health_period}, health check timeout (s) = {self.health_timeout}, ")
        if self.placement_group_strategy != "":
            val += f", placement group strategy = {self.placement_group_strategy}"
        if len(self.placement_group_bundles) > 0:
            val += f", placement group bundles = {','.join([str(bundle) for bundle in self.placement_group_bundles])}"
        if self.replicas_node > 0:
            val += f", replicas per node = {self.replicas_node}"
        if self.queries > 0:
            val += f", concurrent queries = {self.queries}"
        if len(self.user_config) > 0:
            val += f", user config = {','.join(self.user_config)}"
        if len(self.autoscaling_config) > 0:
            val += f", autoscaling config = {','.join(self.autoscaling_config)}"
        if self.actor is not None:
            val += f", actor options = {self.actor.to_string()}"
        return val


class DeploymentApplicationDetails:
    """
        DeploymentApplicationDetails provides detailed info about a deployment within a Serve application.
        It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

        Methods:
        - Create DeploymentApplicationDetails: gets the following parameters:
            name - deployment name
            status - current deployment status
            message - deployment issues details
            deploymentConfig - set of deployment config options
            replicas - Details about the live replicas of this deployment
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.name = dst.get("name", "")
        self.status = dst.get("status", "")
        self.message = dst.get("message", "")
        self.config = DeploymentSchema(dst.get("deploymentConfig"))
        self.replicas = [Replica(r) for r in dst.get("replicas", [])]

    def to_string(self) -> str:
        val = f"name = {self.name}, status = {self.status}"
        if self.message != "":
            val += f", message = {self.message}"
        val += f", config = {self.config.to_string()}, replicas : "
        for replica in self.replicas:
            val += f"\n {replica.to_string()}"
        return val


class ServeDeploymentDetails:
    """
        ServeDeploymentDetails describes one Serve application.
        It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

        Methods:
        - Create ServeDeploymentDetails: gets the following parameters:
            name - deployment name
            args - arguments that will be passed to the application builder
            host - host for HTTP servers to listen on.
            port - port for HTTP server.
            importPath - an import path to a bound deployment node
            routePrefix - Route prefix for HTTP requests
            runtimeEnv - runtime_env that the deployment graph will be run in
            deployments - deployment options that override options specified in the code
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.name = dst.get("name", "")
        self.args = dst.get("args", {})
        self.host = dst.get("host", "")
        self.port = dst.get("port", 0)
        self.importPath = dst.get("importPath", "")
        self.routePrefix = dst.get("routePrefix", "")
        self.runtimeEnv = dst.get("runtimeEnv", {})
        self.deployments = [DeploymentSchema(d) for d in dst.get("deployments", [])]

    def to_string(self) -> str:
        val = f"name = {self.name}, import path = {self.importPath}, route prefix = {self.routePrefix}"
        if len(self.args) > 0:
            val += f", args = {str(self.args)}"
        if self.host != "":
            val += f", host = {str(self.host)}"
        if self.port > 0:
            val += f", port = {str(self.port)}"
        if len(self.runtimeEnv) > 0:
            val += f", runtime env = {str(self.runtimeEnv)}"
        for deployment in self.deployments:
            val += f"\n {deployment.to_string()}"
        return val


class ServeApplicationDetails:
    """
        ServeApplicationDetails provides detailed info about a deployment within a Serve application.
        It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

        Methods:
        - Create ServeApplicationDetails: gets the following parameters:
            name - application name
            status - current application status
            message - message that gives more insight into the application status
            routePrefix - the route_prefix of the ingress deployment in the application
            docsPath - the path at which the docs for this application is served
            lastDeployedTimeS - last application deployment time
            deployments - details of the application deployments
            deployedAppConfig - exact copy of the application config that was submitted to the cluster.
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.name = dst.get("name", "")
        self.status = dst.get("status", "")
        self.message = dst.get("message", "")
        self.route_prefix = dst.get("routePrefix", "")
        self.doc_path = dst.get("docsPath", "")
        self.deployed = dst.get("lastDeployedTimeS", 0)
        deployments = {}
        for k, v in dst.get("deployments", {}).items():
            deployments[k] = DeploymentApplicationDetails(v)
        self.deployments = deployments
        conf = dst.get("deployedAppConfig", None)
        if conf is not None:
            self.config = ServeDeploymentDetails(conf)
        else:
            self.config = None

    def to_string(self) -> str:
        val = f"name = {self.name}, status = {self.status}, last deployed (s) = {self.deployed}"
        if self.message != "":
            val += f", message = {self.message}"
        if self.route_prefix != "":
            val += f", route prefix = {self.route_prefix}"
        if self.doc_path != "":
            val += f", doc path = {self.doc_path}"
        val += ", deployments:"
        for k, v in self.deployments.items():
            val += f"\n {k} = {v.to_string()}"
        if self.config is not None:
            val += f"\n deployed app config = {self.config.to_string()}"

        return val


class ServeInstance:
    """
        ServeInstance provides serve metadata with system-level info and details on all applications deployed to
        the Ray cluster
        It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

        Methods:
        - Create ServeInstance: gets the following parameters:
            deployMode - deployment mode
            proxyLocation - location of proxies
            controllerInfo - details about all live applications running on the cluster
            httpOptions - HTTP Proxy options
            grpcOptions - GRPC Proxy options
            applications - details about all live applications running on the cluster
            proxies - mapping from node_id to details about the Proxy running on that node
    """

    def __init__(self, dst: dict[str, any]) -> None:
        self.deploy_mode = dst.get("deployMode", "")
        self.proxy_location = dst.get("proxyLocation", "")
        self.controller_info = ControllerInfo(dst.get("controllerInfo", {}))
        self.http = HTTPOptions(dst.get("httpOptions", {}))
        self.grpc = GRPCOptions(dst.get("grpcOptions", {}))
        apps = {}
        for k, v in dst.get("applications", {}).items():
            apps[k] = ServeApplicationDetails(v)
        self.applications = apps
        proxies = {}
        for k, v in dst.get("proxies", {}).items():
            proxies[k] = Proxy(v)
        self.proxies = proxies

    def to_string(self) -> str:
        val = f"deploy mode = {self.deploy_mode}, proxy locations = {self.proxy_location}"
        val += f"\n controller info = {self.controller_info.to_string()}"
        val += f"\n HTTP options = {self.http.to_string()}"
        val += f"\n GRPC options = {self.grpc.to_string()}, applications ="
        for k, v in self.applications.items():
            val += f"\n {k} = {v.to_string()}"
        for k, v in self.proxies.items():
            val += f"\n {k} = {v.to_string()}"
        return val

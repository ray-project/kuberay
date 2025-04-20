<!-- markdownlint-disable MD013 -->
# Support proto Core API and RESTful backend services

## Motivation

There're few major blockers for users to use KubeRay Operator directly.

- Current ray operator is only friendly to users who is familiar with Kubernetes operator pattern. For most data scientists, there's still a learning curve.

- Using kubectl requires sophisticated permission system. Some kubernetes clusters do not enable user level authentication. In some companies, devops use loose RBAC management and corp SSO system is not integrated with Kubernetes OIDC at all.

For the above reasons, it's worth it to build a generic abstraction on top of the RayCluster CRD. With the core API support, we can easily build backend services, cli, etc to bridge users without Kubernetes experience to KubeRay.

## Goals

- The API definition should be flexible enough to support different kinds of clients (e.g. backend, cli etc).
- This backend service underneath should leverage generate clients to interact with existing RayCluster custom resources.
- New added components should be plugable to existing operator.

## Proposal

### Deployment topology and interactive flow

The new gRPC service would be a individual deployment of the KubeRay control plane and user can choose to install it optionally. It will create a service and exposes endpoint to users.

```text
NAME                                                      READY   STATUS    RESTARTS      AGE
kuberay-grpc-service-c8db9dc65-d4w5r                      1/1     Running   0             2d15h
kuberay-operator-785476b948-fmlm7                         1/1     Running   0             3d
```

In issue [#29](https://github.com/ray-project/kuberay/issues/29), `RayCluster` CRD clientset has been generated and gRPC service can leverage it to operate Custom Resources.

A simple flow would be like this. (Thanks [@akanso](https://github.com/akanso) for providing the flow)

```text
client --> GRPC Server --> [created Custom Resources] <-- Ray Operator (reads CR and accordingly performs CRUD)
```

### API abstraction

Protocol Buffers are a language-neutral, platform-neutral extensible mechanism for serializing structured data. Protoc also provides different community plugins to meet different needs.

In order to better define resources at the API level, a few proto files will be defined. Technically, we can use similar data structure like `RayCluster` Kubernetes resource but this is probably not a good idea.

- Some of the Kubernetes API like `tolerance` and `node affinity` are too complicated to be converted to an API.
- We want to leave some flexibility to use database to store history data in the near future (for example, pagination, list options etc).

To resolve these issues, we provide a simple API which can cover most common use-cases.

For example, the protobuf definition of the `RayCluster`:

```proto
service ClusterService {
  // Creates a new Cluster.
  rpc CreateCluster(CreateClusterRequest) returns (Cluster) {
    option (google.api.http) = {
      post: "/apis/v1alpha2/namespaces/{namespace}/clusters"
      body: "cluster"
    };
  }

  // Finds a specific Cluster by ID.
  rpc GetCluster(GetClusterRequest) returns (Cluster) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/clusters/{name}"
    };
  }

  // Finds all Clusters in a given namespace.
  rpc ListCluster(ListClustersRequest) returns (ListClustersResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/clusters"
    };
  }

  // Finds all Clusters in all namespaces.
  rpc ListAllClusters(ListAllClustersRequest) returns (ListAllClustersResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/clusters"
    };
  }

  // Deletes an cluster without deleting the cluster's runs and jobs. To
  // avoid unexpected behaviors, delete an cluster's runs and jobs before
  // deleting the cluster.
  rpc DeleteCluster(DeleteClusterRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/apis/v1alpha2/namespaces/{namespace}/clusters/{name}"
    };
  }
}

message CreateClusterRequest {
  // The cluster to be created.
  Cluster cluster = 1;
  // The namespace of the cluster to be created.
  string namespace = 2;
}

message GetClusterRequest {
  // The name of the cluster to be retrieved.
  string name = 1;
  // The namespace of the cluster to be retrieved.
  string namespace = 2;
}

message ListClustersRequest {
  // The namespace of the clusters to be retrieved.
  string namespace = 1;

}

message ListClustersResponse {
  // A list of clusters returned.
  repeated Cluster clusters = 1;
}

message ListAllClustersRequest {}

message ListAllClustersResponse {
  // A list of clusters returned.
  repeated Cluster clusters = 1;
}

message DeleteClusterRequest {
  // The name of the cluster to be deleted.
  string name = 1;
  // The namespace of the cluster to be deleted.
  string namespace = 2;
}

message Cluster {
  // Required input field. Unique cluster name provided by user.
  string name = 1;

  // Required input field. Cluster's namespace provided by user
  string namespace = 2;

  // Required field. This field indicates the user who owns the cluster.
  string user = 3;

  // Optional input field. Ray cluster version
  string version = 4;

  // Optional field.
  enum Environment {
    DEV = 0;
    TESTING = 1;
    STAGING = 2;
    PRODUCTION = 3;
  }
  Environment environment = 5;

  // Required field. This field indicates ray cluster configuration
  ClusterSpec cluster_spec = 6;

  // Output. The time that the cluster created.
  google.protobuf.Timestamp created_at = 7;

  // Output. The time that the cluster deleted.
  google.protobuf.Timestamp deleted_at = 8;

  // Output. The status to show the cluster status.state
  string cluster_state = 9;

  // Output. The list related to the cluster.
  repeated ClusterEvent events = 10;

  // Output. The service endpoint of the cluster
  map<string, string> service_endpoint = 11;

  // Optional input field. Container environment variables from user.
  map<string, string> envs = 12;
}

message ClusterSpec {
  // The head group configuration
  HeadGroupSpec head_group_spec = 1;
  // The worker group configurations
  repeated WorkerGroupSpec worker_group_spec = 2;
}

message Volume {
  string mount_path = 1;
  enum VolumeType {
    PERSISTENT_VOLUME_CLAIM = 0;
    HOST_PATH = 1;
  }
  VolumeType volume_type = 2;
  string name = 3;
  string source = 4;
  bool read_only = 5;

  // If indicate hostpath, we need to let user indicate which type
  // they would like to use.
  enum HostPathType {
    DIRECTORY = 0;
    FILE = 1;
  }
  HostPathType host_path_type = 6;

  enum MountPropagationMode {
    NONE = 0;
    HOSTTOCONTAINER = 1;
    BIDIRECTIONAL = 2;
  }
  MountPropagationMode mount_propagation_mode = 7;
}

message HeadGroupSpec {
  // Optional. The computeTemplate of head node group
  string compute_template = 1;
  // Optional field. This field will be used to retrieve right ray container
  string image = 2;
  // Optional. The service type (ClusterIP, NodePort, Load balancer) of the head node
  string service_type = 3;
  // Optional. The ray start params of head node group
  map<string, string> ray_start_params = 4;
  // Optional. The volumes mount to head pod
  repeated Volume volumes = 5;
}

message WorkerGroupSpec {
  // Required. Group name of the current worker group
  string group_name = 1;
  // Optional. The computeTemplate of head node group
  string compute_template = 2;
  // Optional field. This field will be used to retrieve right ray container
  string image = 3;
  // Required. Desired replicas of the worker group
  int32 replicas = 4;
  // Optional. Min replicas of the worker group
  int32 min_replicas = 5;
  // Optional. Max replicas of the worker group
  int32 max_replicas = 6;
  // Optional. The ray start parames of worker node group
  map<string, string> ray_start_params = 7;
  // Optional. The volumes mount to worker pods
  repeated Volume volumes = 8;
}

message ClusterEvent {
  // Output. Unique Event Id.
  string id = 1;

  // Output. Human readable name for event.
  string name = 2;

  // Output. The creation time of the event.
  google.protobuf.Timestamp created_at = 3;

  // Output. The last time the event occur.
  google.protobuf.Timestamp first_timestamp = 4;

  // Output. The first time the event occur
  google.protobuf.Timestamp last_timestamp = 5;

  // Output. The reason for the transition into the object's current status.
  string reason = 6;

  // Output. A human-readable description of the status of this operation.
  string message = 7;

  // Output. Type of this event (Normal, Warning), new types could be added in the future
  string type = 8;

  // Output. The number of times this event has occurred.
  int32 count = 9;
}

```

### Support multiple clients

Since we may have different clients to interactive with our services, we will generate gateway RESTful APIs and OpenAPI Spec at the same time.

![proto](https://user-images.githubusercontent.com/4739316/136441815-6bbf7b91-4bf9-4e89-a770-71e02f451987.png)

> `.proto` define core api, grpc and gateway services. go_client and swagger can be generated easily for further usage.

### gRPC services

The GRPC protocol provides an extremely efficient way of cross-service communication for distributed applications. The public toolkit includes instruments to generate client and server code-bases for many languages allowing the developer to use the most optimal language for the task.

The service will implement gPRC server as following graph shows.

- A `ResourceManager` will be used to abstract the implementation of CRUD operators.
- ClientManager manages kubernetes clients which can operate Kubernetes native resource and custom resources like RayCluster.
- `RayClusterClient` comes from code generator of CRD. [issue#29](https://github.com/ray-project/kuberay/issues/29)

![service architecture](https://user-images.githubusercontent.com/4739316/135743532-688f09e8-c322-4b78-a332-98d604a24b42.png)

## Implementation History

- 2021-11-25: inital proposal accepted.
- 2022-12-01: new protobuf definition released.

> Note: we should update doc when there's a large update.

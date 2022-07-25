# Support proto Core API and RESTful backend services

## Motivation

There're few major blockers for users to use KubeRay Operator directly.

- Current ray operator is only friendly to users who is familiar with Kubernetes operator pattern. For most data scientists, there's still a learning curve.

- Using kubectl requires sophisticated permission system. Some kubernetes clusters do not enable user level authentication. In some companies, devops use loose RBAC management and corp SSO system is not integrated with Kubernetes OIDC at all.

Due to above reason, it's worth to build generic abstraction on top of RayCluster CRD. With the core api support, we can easily build backend services, cli, etc to bridge users without Kubernetes experiences to KubeRay.

## Goals

- The api definition should be flexible enough to support different kinds of clients (e.g. backend, cli etc).
- This backend service underneath should leverage generate clients to interact with existing RayCluster custom resources.
- New added components should be plugable to existing operator.

## Proposal

### Deployment topology and interactive flow

The new gRPC service would be a individual deployment of the KubeRay control plane and user can choose to install it optionally. It will create a service and exposes endpoint to users.

```
NAME                                                      READY   STATUS    RESTARTS      AGE
kuberay-grpc-service-c8db9dc65-d4w5r                      1/1     Running   0             2d15h
kuberay-operator-785476b948-fmlm7                         1/1     Running   0             3d
```

In issue [#29](https://github.com/ray-project/kuberay/issues/29), `RayCluster` CRD clientset has been generated and gRPC service can leverage it to operate Custom Resources.

A simple flow would be like this. (Thanks [@akanso](https://github.com/akanso) for providing the flow)
```
client --> GRPC Server --> [created Custom Resources] <-- Ray Operator (reads CR and accordingly performs CRUD)
```

### API abstraction

Protocol Buffers are a language-neutral, platform-neutral extensible mechanism for serializing structured data. Protoc also provides different community plugins to meet different needs.

In order to better define resources at the API level, a few proto files will be defined. Technically, we can use similar data structure like `RayCluster` Kubernetes resource but this is probably not a good idea.

- Some of the Kubernetes API like `tolerance` and `node affinity` are too complicated to be converted to an API.
- We want to leave some flexibility to use database to store history data in the near future (for example, pagination, list options etc).

We end up propsing a simple and easy API which can cover most of the daily requirements.

```
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

  // Finds all Clusters in a given namespace. Supports pagination, and sorting on certain fields.
  rpc ListCluster(ListClustersRequest) returns (ListClustersResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/clusters"
    };
  }

  // Finds all Clusters in all namespaces. Supports pagination, and sorting on certain fields.
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
}

message ClusterSpec {
  // The head group configuration
  HeadGroupSpec head_group_spec = 1;
  // The worker group configurations
  repeated WorkerGroupSpec worker_group_spec = 2;
}

message HeadGroupSpec {
  // Optional. The computeTemplate of head node group
  string compute_template = 1;
  // Optional field. This field will be used to retrieve right ray container
  string image = 2;
  // Optional. The service type (ClusterIP, NodePort, Load balancer) of the head node
  string service_type = 3;
  // Optional. The ray start parames of head node group
  map<string, string> ray_start_params = 4;
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
}

service ComputeTemplateService {
  // Creates a new compute template.
  rpc CreateComputeTemplate(CreateComputeTemplateRequest) returns (ComputeTemplate) {
    option (google.api.http) = {
      post: "/apis/v1alpha2/compute_templates"
      body: "compute_template"
    };
  }

  // Finds a specific compute template by its name and namespace.
  rpc GetComputeTemplate(GetComputeTemplateRequest) returns (ComputeTemplate) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/compute_templates/{name}"
    };
  }

  // Finds all compute templates in a given namespace. Supports pagination, and sorting on certain fields.
  rpc ListComputeTemplates(ListComputeTemplatesRequest) returns (ListComputeTemplatesResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/compute_templates"
    };
  }

  // Finds all compute templates in all namespaces. Supports pagination, and sorting on certain fields.
  rpc ListAllComputeTemplates(ListAllComputeTemplatesRequest) returns (ListAllComputeTemplatesResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/compute_templates"
    };
  }

  // Deletes a compute template by its name and namespace 
  rpc DeleteComputeTemplate(DeleteComputeTemplateRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/apis/v1alpha2/namespaces/{namespace}/compute_templates/{name}"
    };
  }
}

message CreateComputeTemplateRequest {
  // The compute template to be created.
  ComputeTemplate compute_template = 1;
  // The namespace of the compute template to be created
  string namespace = 2;
}

message GetComputeTemplateRequest {
  // The name of the ComputeTemplate to be retrieved.
  string name = 1;
  // The namespace of the compute template to be retrieved.
  string namespace = 2;
}

message ListComputeTemplatesRequest {
  // The namespace of the compute templates to be retrieved. 
  string namespace = 1;
  // TODO: support paganation later
}

message ListComputeTemplatesResponse {
  repeated ComputeTemplate compute_templates = 1;
}

message ListAllComputeTemplatesRequest {
  // TODO: support paganation later
}

message ListAllComputeTemplatesResponse {
  repeated ComputeTemplate compute_templates = 1;
}

message DeleteComputeTemplateRequest {
  // The name of the compute template to be deleted.
  string name = 1;
  // The namespace of the compute template to be deleted.
  string namespace = 2;
}

// ComputeTemplate can be reused by any compute units like worker group, workspace, image build job, etc
message ComputeTemplate {
  // The name of the compute template
  string name = 1;
  // The namespace of the compute template
  string namespace = 2;
  // Number of cpus
  uint32 cpu = 3;
  // Number of memory
  uint32 memory = 4;
  // Number of gpus
  uint32 gpu = 5;
  // The detail gpu accelerator type
  string gpu_accelerator = 6;
}


service ImageTemplateService {
  // Creates a new ImageTemplate.
  rpc CreateImageTemplate(CreateImageTemplateRequest) returns (ImageTemplate) {
    option (google.api.http) = {
      post: "/apis/v1alpha2/image_templates"
      body: "image_template"
    };
  }

  // Finds a specific ImageTemplate by ID.
  rpc GetImageTemplate(GetImageTemplateRequest) returns (ImageTemplate) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/image_templates/{name}"
    };
  }

  // Finds all ImageTemplates. Supports pagination, and sorting on certain fields.
  rpc ListImageTemplates(ListImageTemplatesRequest) returns (ListImageTemplatesResponse) {
    option (google.api.http) = {
      get: "/apis/v1alpha2/namespaces/{namespace}/image_templates"
    };
  }

  // Deletes an ImageTemplate.
  rpc DeleteImageTemplate(DeleteImageTemplateRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/apis/v1alpha2/namespaces/{namespace}/image_templates/{name}"
    };
  }
}

message CreateImageTemplateRequest {
  // The image template to be created.
  ImageTemplate image_template = 1;
  // The namespace of the image template to be created.
  string namespace = 2;
}

message GetImageTemplateRequest {
  // The name of the image template to be retrieved.
  string name = 1;
  // The namespace of the image template to be retrieved.
  string namespace = 2;
}

message ListImageTemplatesRequest {
  // The namespace of the image templates to be retrieved.
  string namespace = 1;
  // TODO: support pagingation later
}

message ListImageTemplatesResponse {
  // A list of Compute returned.
  repeated ImageTemplate image_templates = 1;
}

message ListAllImageTemplatesRequest {
  // TODO: support pagingation later
}

message ListAllImageTemplatesResponse {
  // A list of Compute returned.
  repeated ImageTemplate image_templates = 1;
}

message DeleteImageTemplateRequest {
  // The name of the image template to be deleted.
  string name = 1;
  // The namespace of the image template to be deleted.
  string namespace = 2;
}

// ImageTemplate can be used by worker group and workspce.
// They can be distinguish by different entrypoints
message ImageTemplate {
  // The ID of the image template
  string name = 1;
  // The namespace of the image template
  string namespace = 2;
  // The base container image to be used for image building
  string base_image = 3;
  // The pip packages to install
  repeated string pip_packages = 4;
  // The conda packages to install
  repeated string conda_packages = 5;
  // The system packages to install
  repeated string system_packages = 6;
  // The environment variables to set
  map<string, string> environment_variables = 7;
  // The post install commands to execute
  string custom_commands = 8;
  // Output. The result image generated
  string image = 9;
}

message Status {
  string error = 1;
  int32 code = 2;
  repeated google.protobuf.Any details = 3;
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

> Note: we should update doc when there's a large update.

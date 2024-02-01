# API Reference

## Packages
- [ray.io/v1](#rayiov1)
- [ray.io/v1alpha1](#rayiov1alpha1)


## ray.io/v1

Package v1 contains API Schema definitions for the ray v1 API group

### Resource Types
- [RayCluster](#raycluster)
- [RayJob](#rayjob)
- [RayService](#rayservice)



#### AutoscalerOptions



AutoscalerOptions specifies optional configuration for the Ray autoscaler.

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core)_ | Resources specifies optional resource request and limit overrides for the autoscaler container. Default values: 500m CPU request and limit. 512Mi memory request and limit. |
| `image` _string_ | Image optionally overrides the autoscaler's container image. This override is for provided for autoscaler testing and development. |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pullpolicy-v1-core)_ | ImagePullPolicy optionally overrides the autoscaler container's image pull policy. This override is for provided for autoscaler testing and development. |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core) array_ | Optional list of environment variables to set in the autoscaler container. |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core) array_ | Optional list of sources to populate environment variables in the autoscaler container. |
| `volumeMounts` _[VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#volumemount-v1-core) array_ | Optional list of volumeMounts.  This is needed for enabling TLS for the autoscaler container. |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core)_ | SecurityContext defines the security options the container should be run with. If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext. More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/ |
| `idleTimeoutSeconds` _integer_ | IdleTimeoutSeconds is the number of seconds to wait before scaling down a worker pod which is not using Ray resources. Defaults to 60 (one minute). It is not read by the KubeRay operator but by the Ray autoscaler. |
| `upscalingMode` _[UpscalingMode](#upscalingmode)_ | UpscalingMode is "Conservative", "Default", or "Aggressive." Conservative: Upscaling is rate-limited; the number of pending worker pods is at most the size of the Ray cluster. Default: Upscaling is not rate-limited. Aggressive: An alias for Default; upscaling is not rate-limited. It is not read by the KubeRay operator but by the Ray autoscaler. |




#### HeadGroupSpec



HeadGroupSpec are the spec for the head pod

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#servicetype-v1-core)_ | ServiceType is Kubernetes service type of the head service. it will be used by the workers to connect to the head pod |
| `headService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | HeadService is the Kubernetes service of the head pod. |
| `enableIngress` _boolean_ | EnableIngress indicates whether operator should create ingress object for head service or not. |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: node-manager-port, object-store-memory, ... |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is the exact pod template used in K8s depoyments, statefulsets, etc. |


#### JobSubmissionMode

_Underlying type:_ _string_



_Appears in:_
- [RayJobSpec](#rayjobspec)



#### RayCluster



RayCluster is the Schema for the RayClusters API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1`
| `kind` _string_ | `RayCluster`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayClusterSpec](#rayclusterspec)_ | Specification of the desired behavior of the RayCluster. |


#### RayClusterSpec



RayClusterSpec defines the desired state of RayCluster

_Appears in:_
- [RayCluster](#raycluster)
- [RayJobSpec](#rayjobspec)
- [RayServiceSpec](#rayservicespec)

| Field | Description |
| --- | --- |
| `headGroupSpec` _[HeadGroupSpec](#headgroupspec)_ | INSERT ADDITIONAL SPEC FIELDS - desired state of cluster Important: Run "make" to regenerate code after modifying this file HeadGroupSpecs are the spec for the head pod |
| `workerGroupSpecs` _[WorkerGroupSpec](#workergroupspec) array_ | WorkerGroupSpecs are the specs for the worker pods |
| `rayVersion` _string_ | RayVersion is used to determine the command for the Kubernetes Job managed by RayJob |
| `enableInTreeAutoscaling` _boolean_ | EnableInTreeAutoscaling indicates whether operator should create in tree autoscaling configs |
| `autoscalerOptions` _[AutoscalerOptions](#autoscaleroptions)_ | AutoscalerOptions specifies optional configuration for the Ray autoscaler. |
| `headServiceAnnotations` _object (keys:string, values:string)_ |  |
| `suspend` _boolean_ | Suspend indicates whether a RayCluster should be suspended. A suspended RayCluster will have head pods and worker pods deleted. |


#### RayJob



RayJob is the Schema for the rayjobs API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1`
| `kind` _string_ | `RayJob`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayJobSpec](#rayjobspec)_ |  |


#### RayJobSpec



RayJobSpec defines the desired state of RayJob

_Appears in:_
- [RayJob](#rayjob)

| Field | Description |
| --- | --- |
| `entrypoint` _string_ | INSERT ADDITIONAL SPEC FIELDS - desired state of cluster Important: Run "make" to regenerate code after modifying this file |
| `metadata` _object (keys:string, values:string)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `runtimeEnvYAML` _string_ | RuntimeEnvYAML represents the runtime environment configuration provided as a multi-line YAML string. |
| `jobId` _string_ | If jobId is not set, a new jobId will be auto-generated. |
| `shutdownAfterJobFinishes` _boolean_ | ShutdownAfterJobFinishes will determine whether to delete the ray cluster once rayJob succeed or failed. |
| `ttlSecondsAfterFinished` _integer_ | TTLSecondsAfterFinished is the TTL to clean up RayCluster. It's only working when ShutdownAfterJobFinishes set to true. |
| `rayClusterSpec` _[RayClusterSpec](#rayclusterspec)_ | RayClusterSpec is the cluster template to run the job |
| `clusterSelector` _object (keys:string, values:string)_ | clusterSelector is used to select running rayclusters by labels |
| `submissionMode` _[JobSubmissionMode](#jobsubmissionmode)_ | SubmissionMode specifies how RayJob submits the Ray job to the RayCluster. In "K8sJobMode", the KubeRay operator creates a submitter Kubernetes Job to submit the Ray job. In "HTTPMode", the KubeRay operator sends a request to the RayCluster to create a Ray job. |
| `suspend` _boolean_ | suspend specifies whether the RayJob controller should create a RayCluster instance If a job is applied with the suspend field set to true, the RayCluster will not be created and will wait for the transition to false. If the RayCluster is already created, it will be deleted. In case of transition to false a new RayCluster will be created. |
| `submitterPodTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | SubmitterPodTemplate is the template for the pod that will run `ray job submit`. |
| `entrypointNumCpus` _float_ | EntrypointNumCpus specifies the number of cpus to reserve for the entrypoint command. |
| `entrypointNumGpus` _float_ | EntrypointNumGpus specifies the number of gpus to reserve for the entrypoint command. |
| `entrypointResources` _string_ | EntrypointResources specifies the custom resources and quantities to reserve for the entrypoint command. |




#### RayService



RayService is the Schema for the rayservices API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1`
| `kind` _string_ | `RayService`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayServiceSpec](#rayservicespec)_ |  |


#### RayServiceSpec



RayServiceSpec defines the desired state of RayService

_Appears in:_
- [RayService](#rayservice)

| Field | Description |
| --- | --- |
| `serveConfigV2` _string_ | Important: Run "make" to regenerate code after modifying this file Defines the applications and deployments to deploy, should be a YAML multi-line scalar string. |
| `rayClusterConfig` _[RayClusterSpec](#rayclusterspec)_ |  |
| `serviceUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |
| `deploymentUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |
| `serveService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | ServeService is the Kubernetes service for head node and worker nodes who have healthy http proxy to serve traffics. |




#### ScaleStrategy



ScaleStrategy to remove workers

_Appears in:_
- [WorkerGroupSpec](#workergroupspec)

| Field | Description |
| --- | --- |
| `workersToDelete` _string array_ | WorkersToDelete workers to be deleted |


#### UpscalingMode

_Underlying type:_ _string_



_Appears in:_
- [AutoscalerOptions](#autoscaleroptions)



#### WorkerGroupSpec



WorkerGroupSpec are the specs for the worker pods

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `groupName` _string_ | we can have multiple worker groups, we distinguish them by name |
| `replicas` _integer_ | Replicas is the number of desired Pods for this worker group. See https://github.com/ray-project/kuberay/pull/1443 for more details about the reason for making this field optional. |
| `minReplicas` _integer_ | MinReplicas denotes the minimum number of desired Pods for this worker group. |
| `maxReplicas` _integer_ | MaxReplicas denotes the maximum number of desired Pods for this worker group, and the default value is maxInt32. |
| `numOfHosts` _integer_ | NumOfHosts denotes the number of hosts to create per replica. The default value is 1. |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: address, object-store-memory, ... |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is a pod template for the worker |
| `scaleStrategy` _[ScaleStrategy](#scalestrategy)_ | ScaleStrategy defines which pods to remove |



## ray.io/v1alpha1


Package v1alpha1 contains API Schema definitions for the ray v1alpha1 API group

### Resource Types
- [RayCluster](#raycluster)
- [RayJob](#rayjob)
- [RayService](#rayservice)



#### AutoscalerOptions



AutoscalerOptions specifies optional configuration for the Ray autoscaler.

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core)_ | Resources specifies optional resource request and limit overrides for the autoscaler container. Default values: 500m CPU request and limit. 512Mi memory request and limit. |
| `image` _string_ | Image optionally overrides the autoscaler's container image. This override is for provided for autoscaler testing and development. |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pullpolicy-v1-core)_ | ImagePullPolicy optionally overrides the autoscaler container's image pull policy. This override is for provided for autoscaler testing and development. |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core) array_ | Optional list of environment variables to set in the autoscaler container. |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core) array_ | Optional list of sources to populate environment variables in the autoscaler container. |
| `volumeMounts` _[VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#volumemount-v1-core) array_ | Optional list of volumeMounts.  This is needed for enabling TLS for the autoscaler container. |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core)_ | SecurityContext defines the security options the container should be run with. If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext. More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/ |
| `idleTimeoutSeconds` _integer_ | IdleTimeoutSeconds is the number of seconds to wait before scaling down a worker pod which is not using Ray resources. Defaults to 60 (one minute). It is not read by the KubeRay operator but by the Ray autoscaler. |
| `upscalingMode` _[UpscalingMode](#upscalingmode)_ | UpscalingMode is "Conservative", "Default", or "Aggressive." Conservative: Upscaling is rate-limited; the number of pending worker pods is at most the size of the Ray cluster. Default: Upscaling is not rate-limited. Aggressive: An alias for Default; upscaling is not rate-limited. It is not read by the KubeRay operator but by the Ray autoscaler. |




#### HeadGroupSpec



HeadGroupSpec are the spec for the head pod

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#servicetype-v1-core)_ | ServiceType is Kubernetes service type of the head service. it will be used by the workers to connect to the head pod |
| `headService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | HeadService is the Kubernetes service of the head pod. |
| `enableIngress` _boolean_ | EnableIngress indicates whether operator should create ingress object for head service or not. |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: node-manager-port, object-store-memory, ... |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is the exact pod template used in K8s depoyments, statefulsets, etc. |


#### RayCluster



RayCluster is the Schema for the RayClusters API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1`
| `kind` _string_ | `RayCluster`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayClusterSpec](#rayclusterspec)_ | Specification of the desired behavior of the RayCluster. |


#### RayClusterSpec



RayClusterSpec defines the desired state of RayCluster

_Appears in:_
- [RayCluster](#raycluster)
- [RayJobSpec](#rayjobspec)
- [RayServiceSpec](#rayservicespec)

| Field | Description |
| --- | --- |
| `headGroupSpec` _[HeadGroupSpec](#headgroupspec)_ | INSERT ADDITIONAL SPEC FIELDS - desired state of cluster Important: Run "make" to regenerate code after modifying this file HeadGroupSpecs are the spec for the head pod |
| `workerGroupSpecs` _[WorkerGroupSpec](#workergroupspec) array_ | WorkerGroupSpecs are the specs for the worker pods |
| `rayVersion` _string_ | RayVersion is used to determine the command for the Kubernetes Job managed by RayJob |
| `enableInTreeAutoscaling` _boolean_ | EnableInTreeAutoscaling indicates whether operator should create in tree autoscaling configs |
| `autoscalerOptions` _[AutoscalerOptions](#autoscaleroptions)_ | AutoscalerOptions specifies optional configuration for the Ray autoscaler. |
| `headServiceAnnotations` _object (keys:string, values:string)_ |  |
| `suspend` _boolean_ | Suspend indicates whether a RayCluster should be suspended. A suspended RayCluster will have head pods and worker pods deleted. |


#### RayJob



RayJob is the Schema for the rayjobs API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1`
| `kind` _string_ | `RayJob`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayJobSpec](#rayjobspec)_ |  |


#### RayJobSpec



RayJobSpec defines the desired state of RayJob

_Appears in:_
- [RayJob](#rayjob)

| Field | Description |
| --- | --- |
| `entrypoint` _string_ | INSERT ADDITIONAL SPEC FIELDS - desired state of cluster Important: Run "make" to regenerate code after modifying this file |
| `metadata` _object (keys:string, values:string)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `runtimeEnvYAML` _string_ | RuntimeEnvYAML represents the runtime environment configuration provided as a multi-line YAML string. |
| `jobId` _string_ | If jobId is not set, a new jobId will be auto-generated. |
| `shutdownAfterJobFinishes` _boolean_ | ShutdownAfterJobFinishes will determine whether to delete the ray cluster once rayJob succeed or failed. |
| `ttlSecondsAfterFinished` _integer_ | TTLSecondsAfterFinished is the TTL to clean up RayCluster. It's only working when ShutdownAfterJobFinishes set to true. |
| `rayClusterSpec` _[RayClusterSpec](#rayclusterspec)_ | RayClusterSpec is the cluster template to run the job |
| `clusterSelector` _object (keys:string, values:string)_ | clusterSelector is used to select running rayclusters by labels |
| `suspend` _boolean_ | suspend specifies whether the RayJob controller should create a RayCluster instance If a job is applied with the suspend field set to true, the RayCluster will not be created and will wait for the transition to false. If the RayCluster is already created, it will be deleted. In case of transition to false a new RayCluster will be created. |
| `submitterPodTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | SubmitterPodTemplate is the template for the pod that will run `ray job submit`. |
| `entrypointNumCpus` _float_ | EntrypointNumCpus specifies the number of cpus to reserve for the entrypoint command. |
| `entrypointNumGpus` _float_ | EntrypointNumGpus specifies the number of gpus to reserve for the entrypoint command. |
| `entrypointResources` _string_ | EntrypointResources specifies the custom resources and quantities to reserve for the entrypoint command. |




#### RayService



RayService is the Schema for the rayservices API



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1`
| `kind` _string_ | `RayService`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[RayServiceSpec](#rayservicespec)_ |  |


#### RayServiceSpec



RayServiceSpec defines the desired state of RayService

_Appears in:_
- [RayService](#rayservice)

| Field | Description |
| --- | --- |
| `serveConfigV2` _string_ | Important: Run "make" to regenerate code after modifying this file Defines the applications and deployments to deploy, should be a YAML multi-line scalar string. |
| `rayClusterConfig` _[RayClusterSpec](#rayclusterspec)_ |  |
| `serviceUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |
| `deploymentUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |
| `serveService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | ServeService is the Kubernetes service for head node and worker nodes who have healthy http proxy to serve traffics. |




#### ScaleStrategy



ScaleStrategy to remove workers

_Appears in:_
- [WorkerGroupSpec](#workergroupspec)

| Field | Description |
| --- | --- |
| `workersToDelete` _string array_ | WorkersToDelete workers to be deleted |


#### UpscalingMode

_Underlying type:_ _string_



_Appears in:_
- [AutoscalerOptions](#autoscaleroptions)



#### WorkerGroupSpec



WorkerGroupSpec are the specs for the worker pods

_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description |
| --- | --- |
| `groupName` _string_ | we can have multiple worker groups, we distinguish them by name |
| `replicas` _integer_ | Replicas is the number of desired Pods for this worker group. See https://github.com/ray-project/kuberay/pull/1443 for more details about the reason for making this field optional. |
| `minReplicas` _integer_ | MinReplicas denotes the minimum number of desired Pods for this worker group. |
| `maxReplicas` _integer_ | MaxReplicas denotes the maximum number of desired Pods for this worker group, and the default value is maxInt32. |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: address, object-store-memory, ... |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is a pod template for the worker |
| `scaleStrategy` _[ScaleStrategy](#scalestrategy)_ | ScaleStrategy defines which pods to remove |



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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core)_ | Resources specifies optional resource request and limit overrides for the autoscaler container.<br />Default values: 500m CPU request and limit. 512Mi memory request and limit. |  |  |
| `image` _string_ | Image optionally overrides the autoscaler's container image. This override is for provided for autoscaler testing and development. |  |  |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pullpolicy-v1-core)_ | ImagePullPolicy optionally overrides the autoscaler container's image pull policy. This override is for provided for autoscaler testing and development. |  |  |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core)_ | SecurityContext defines the security options the container should be run with.<br />If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext.<br />More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/ |  |  |
| `idleTimeoutSeconds` _integer_ | IdleTimeoutSeconds is the number of seconds to wait before scaling down a worker pod which is not using Ray resources.<br />Defaults to 60 (one minute). It is not read by the KubeRay operator but by the Ray autoscaler. |  |  |
| `upscalingMode` _[UpscalingMode](#upscalingmode)_ | UpscalingMode is "Conservative", "Default", or "Aggressive."<br />Conservative: Upscaling is rate-limited; the number of pending worker pods is at most the size of the Ray cluster.<br />Default: Upscaling is not rate-limited.<br />Aggressive: An alias for Default; upscaling is not rate-limited.<br />It is not read by the KubeRay operator but by the Ray autoscaler. |  | Enum: [Default Aggressive Conservative] <br /> |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core) array_ | Optional list of environment variables to set in the autoscaler container. |  |  |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core) array_ | Optional list of sources to populate environment variables in the autoscaler container. |  |  |
| `volumeMounts` _[VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#volumemount-v1-core) array_ | Optional list of volumeMounts.  This is needed for enabling TLS for the autoscaler container. |  |  |


#### DeletionPolicy

_Underlying type:_ _string_





_Appears in:_
- [RayJobSpec](#rayjobspec)





#### GcsFaultToleranceOptions



GcsFaultToleranceOptions contains configs for GCS FT



_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `redisUsername` _[RedisCredential](#rediscredential)_ |  |  |  |
| `redisPassword` _[RedisCredential](#rediscredential)_ |  |  |  |
| `externalStorageNamespace` _string_ |  |  |  |
| `redisAddress` _string_ |  |  |  |


#### HeadGroupSpec



HeadGroupSpec are the spec for the head pod



_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is the exact pod template used in K8s depoyments, statefulsets, etc. |  |  |
| `headService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | HeadService is the Kubernetes service of the head pod. |  |  |
| `enableIngress` _boolean_ | EnableIngress indicates whether operator should create ingress object for head service or not. |  |  |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: node-manager-port, object-store-memory, ... |  |  |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#servicetype-v1-core)_ | ServiceType is Kubernetes service type of the head service. it will be used by the workers to connect to the head pod |  |  |




#### JobSubmissionMode

_Underlying type:_ _string_





_Appears in:_
- [RayJobSpec](#rayjobspec)



#### RayCluster



RayCluster is the Schema for the RayClusters API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1` | | |
| `kind` _string_ | `RayCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayClusterSpec](#rayclusterspec)_ | Specification of the desired behavior of the RayCluster. |  |  |




#### RayClusterSpec



RayClusterSpec defines the desired state of RayCluster



_Appears in:_
- [RayCluster](#raycluster)
- [RayJobSpec](#rayjobspec)
- [RayServiceSpec](#rayservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `suspend` _boolean_ | Suspend indicates whether a RayCluster should be suspended.<br />A suspended RayCluster will have head pods and worker pods deleted. |  |  |
| `managedBy` _string_ | ManagedBy is an optional configuration for the controller or entity that manages a RayCluster.<br />The value must be either 'ray.io/kuberay-operator' or 'kueue.x-k8s.io/multikueue'.<br />The kuberay-operator reconciles a RayCluster which doesn't have this field at all or<br />the field value is the reserved string 'ray.io/kuberay-operator',<br />but delegates reconciling the RayCluster with 'kueue.x-k8s.io/multikueue' to the Kueue.<br />The field is immutable. |  |  |
| `autoscalerOptions` _[AutoscalerOptions](#autoscaleroptions)_ | AutoscalerOptions specifies optional configuration for the Ray autoscaler. |  |  |
| `headServiceAnnotations` _object (keys:string, values:string)_ |  |  |  |
| `enableInTreeAutoscaling` _boolean_ | EnableInTreeAutoscaling indicates whether operator should create in tree autoscaling configs |  |  |
| `gcsFaultToleranceOptions` _[GcsFaultToleranceOptions](#gcsfaulttoleranceoptions)_ | GcsFaultToleranceOptions for enabling GCS FT |  |  |
| `headGroupSpec` _[HeadGroupSpec](#headgroupspec)_ | HeadGroupSpec is the spec for the head pod |  |  |
| `rayVersion` _string_ | RayVersion is used to determine the command for the Kubernetes Job managed by RayJob |  |  |
| `workerGroupSpecs` _[WorkerGroupSpec](#workergroupspec) array_ | WorkerGroupSpecs are the specs for the worker pods |  |  |


#### RayJob



RayJob is the Schema for the rayjobs API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1` | | |
| `kind` _string_ | `RayJob` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayJobSpec](#rayjobspec)_ |  |  |  |


#### RayJobSpec



RayJobSpec defines the desired state of RayJob



_Appears in:_
- [RayJob](#rayjob)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `activeDeadlineSeconds` _integer_ | ActiveDeadlineSeconds is the duration in seconds that the RayJob may be active before<br />KubeRay actively tries to terminate the RayJob; value must be positive integer. |  |  |
| `backoffLimit` _integer_ | Specifies the number of retries before marking this job failed.<br />Each retry creates a new RayCluster. | 0 |  |
| `rayClusterSpec` _[RayClusterSpec](#rayclusterspec)_ | RayClusterSpec is the cluster template to run the job |  |  |
| `submitterPodTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | SubmitterPodTemplate is the template for the pod that will run `ray job submit`. |  |  |
| `metadata` _object (keys:string, values:string)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `clusterSelector` _object (keys:string, values:string)_ | clusterSelector is used to select running rayclusters by labels |  |  |
| `submitterConfig` _[SubmitterConfig](#submitterconfig)_ | Configurations of submitter k8s job. |  |  |
| `managedBy` _string_ | ManagedBy is an optional configuration for the controller or entity that manages a RayJob.<br />The value must be either 'ray.io/kuberay-operator' or 'kueue.x-k8s.io/multikueue'.<br />The kuberay-operator reconciles a RayJob which doesn't have this field at all or<br />the field value is the reserved string 'ray.io/kuberay-operator',<br />but delegates reconciling the RayJob with 'kueue.x-k8s.io/multikueue' to the Kueue.<br />The field is immutable. |  |  |
| `deletionPolicy` _[DeletionPolicy](#deletionpolicy)_ | DeletionPolicy indicates what resources of the RayJob are deleted upon job completion.<br />Valid values are 'DeleteCluster', 'DeleteWorkers', 'DeleteSelf' or 'DeleteNone'.<br />If unset, deletion policy is based on 'spec.shutdownAfterJobFinishes'.<br />This field requires the RayJobDeletionPolicy feature gate to be enabled. |  |  |
| `entrypoint` _string_ | Entrypoint represents the command to start execution. |  |  |
| `runtimeEnvYAML` _string_ | RuntimeEnvYAML represents the runtime environment configuration<br />provided as a multi-line YAML string. |  |  |
| `jobId` _string_ | If jobId is not set, a new jobId will be auto-generated. |  |  |
| `submissionMode` _[JobSubmissionMode](#jobsubmissionmode)_ | SubmissionMode specifies how RayJob submits the Ray job to the RayCluster.<br />In "K8sJobMode", the KubeRay operator creates a submitter Kubernetes Job to submit the Ray job.<br />In "HTTPMode", the KubeRay operator sends a request to the RayCluster to create a Ray job.<br />In "InteractiveMode", the KubeRay operator waits for a user to submit a job to the Ray cluster. | K8sJobMode |  |
| `entrypointResources` _string_ | EntrypointResources specifies the custom resources and quantities to reserve for the<br />entrypoint command. |  |  |
| `entrypointNumCpus` _float_ | EntrypointNumCpus specifies the number of cpus to reserve for the entrypoint command. |  |  |
| `entrypointNumGpus` _float_ | EntrypointNumGpus specifies the number of gpus to reserve for the entrypoint command. |  |  |
| `ttlSecondsAfterFinished` _integer_ | TTLSecondsAfterFinished is the TTL to clean up RayCluster.<br />It's only working when ShutdownAfterJobFinishes set to true. | 0 |  |
| `shutdownAfterJobFinishes` _boolean_ | ShutdownAfterJobFinishes will determine whether to delete the ray cluster once rayJob succeed or failed. |  |  |
| `suspend` _boolean_ | suspend specifies whether the RayJob controller should create a RayCluster instance<br />If a job is applied with the suspend field set to true,<br />the RayCluster will not be created and will wait for the transition to false.<br />If the RayCluster is already created, it will be deleted.<br />In case of transition to false a new RayCluster will be created. |  |  |




#### RayService



RayService is the Schema for the rayservices API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1` | | |
| `kind` _string_ | `RayService` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayServiceSpec](#rayservicespec)_ |  |  |  |






#### RayServiceSpec



RayServiceSpec defines the desired state of RayService



_Appears in:_
- [RayService](#rayservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |  |  |
| `deploymentUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |  |  |
| `serveService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | ServeService is the Kubernetes service for head node and worker nodes who have healthy http proxy to serve traffics. |  |  |
| `upgradeStrategy` _[RayServiceUpgradeStrategy](#rayserviceupgradestrategy)_ | UpgradeStrategy defines the scaling policy used when upgrading the RayService. |  |  |
| `serveConfigV2` _string_ | Important: Run "make" to regenerate code after modifying this file<br />Defines the applications and deployments to deploy, should be a YAML multi-line scalar string. |  |  |
| `rayClusterConfig` _[RayClusterSpec](#rayclusterspec)_ |  |  |  |
| `excludeHeadPodFromServeSvc` _boolean_ | If the field is set to true, the value of the label `ray.io/serve` on the head Pod should always be false.<br />Therefore, the head Pod's endpoint will not be added to the Kubernetes Serve service. |  |  |




#### RayServiceUpgradeStrategy







_Appears in:_
- [RayServiceSpec](#rayservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[RayServiceUpgradeType](#rayserviceupgradetype)_ | Type represents the strategy used when upgrading the RayService. Currently supports `NewCluster` and `None`. |  |  |


#### RayServiceUpgradeType

_Underlying type:_ _string_





_Appears in:_
- [RayServiceUpgradeStrategy](#rayserviceupgradestrategy)



#### RedisCredential



RedisCredential is the redis username/password or a reference to the source containing the username/password



_Appears in:_
- [GcsFaultToleranceOptions](#gcsfaulttoleranceoptions)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `valueFrom` _[EnvVarSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvarsource-v1-core)_ |  |  |  |
| `value` _string_ |  |  |  |


#### ScaleStrategy



ScaleStrategy to remove workers



_Appears in:_
- [WorkerGroupSpec](#workergroupspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `workersToDelete` _string array_ | WorkersToDelete workers to be deleted |  |  |


#### SubmitterConfig







_Appears in:_
- [RayJobSpec](#rayjobspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backoffLimit` _integer_ | BackoffLimit of the submitter k8s job. |  |  |


#### UpscalingMode

_Underlying type:_ _string_



_Validation:_
- Enum: [Default Aggressive Conservative]

_Appears in:_
- [AutoscalerOptions](#autoscaleroptions)



#### WorkerGroupSpec



WorkerGroupSpec are the specs for the worker pods



_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `suspend` _boolean_ | Suspend indicates whether a worker group should be suspended.<br />A suspended worker group will have all pods deleted.<br />This is not a user-facing API and is only used by RayJob DeletionPolicy. |  |  |
| `groupName` _string_ | we can have multiple worker groups, we distinguish them by name |  |  |
| `replicas` _integer_ | Replicas is the number of desired Pods for this worker group. See https://github.com/ray-project/kuberay/pull/1443 for more details about the reason for making this field optional. | 0 |  |
| `minReplicas` _integer_ | MinReplicas denotes the minimum number of desired Pods for this worker group. | 0 |  |
| `maxReplicas` _integer_ | MaxReplicas denotes the maximum number of desired Pods for this worker group, and the default value is maxInt32. | 2147483647 |  |
| `idleTimeoutSeconds` _integer_ | IdleTimeoutSeconds denotes the number of seconds to wait before the v2 autoscaler terminates an idle worker pod of this type.<br />This value is only used with the Ray Autoscaler enabled and defaults to the value set by the AutoscalingConfig if not specified for this worker group. |  |  |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: address, object-store-memory, ... |  |  |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is a pod template for the worker |  |  |
| `scaleStrategy` _[ScaleStrategy](#scalestrategy)_ | ScaleStrategy defines which pods to remove |  |  |
| `numOfHosts` _integer_ | NumOfHosts denotes the number of hosts to create per replica. The default value is 1. | 1 |  |



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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core)_ | Resources specifies optional resource request and limit overrides for the autoscaler container.<br />Default values: 500m CPU request and limit. 512Mi memory request and limit. |  |  |
| `image` _string_ | Image optionally overrides the autoscaler's container image. This override is for provided for autoscaler testing and development. |  |  |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pullpolicy-v1-core)_ | ImagePullPolicy optionally overrides the autoscaler container's image pull policy. This override is for provided for autoscaler testing and development. |  |  |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core)_ | SecurityContext defines the security options the container should be run with.<br />If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext.<br />More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/ |  |  |
| `idleTimeoutSeconds` _integer_ | IdleTimeoutSeconds is the number of seconds to wait before scaling down a worker pod which is not using Ray resources.<br />Defaults to 60 (one minute). It is not read by the KubeRay operator but by the Ray autoscaler. |  |  |
| `upscalingMode` _[UpscalingMode](#upscalingmode)_ | UpscalingMode is "Conservative", "Default", or "Aggressive."<br />Conservative: Upscaling is rate-limited; the number of pending worker pods is at most the size of the Ray cluster.<br />Default: Upscaling is not rate-limited.<br />Aggressive: An alias for Default; upscaling is not rate-limited.<br />It is not read by the KubeRay operator but by the Ray autoscaler. |  | Enum: [Default Aggressive Conservative] <br /> |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core) array_ | Optional list of environment variables to set in the autoscaler container. |  |  |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core) array_ | Optional list of sources to populate environment variables in the autoscaler container. |  |  |
| `volumeMounts` _[VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#volumemount-v1-core) array_ | Optional list of volumeMounts.  This is needed for enabling TLS for the autoscaler container. |  |  |




#### HeadGroupSpec



HeadGroupSpec are the spec for the head pod



_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is the exact pod template used in K8s depoyments, statefulsets, etc. |  |  |
| `headService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | HeadService is the Kubernetes service of the head pod. |  |  |
| `enableIngress` _boolean_ | EnableIngress indicates whether operator should create ingress object for head service or not. |  |  |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: node-manager-port, object-store-memory, ... |  |  |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#servicetype-v1-core)_ | ServiceType is Kubernetes service type of the head service. it will be used by the workers to connect to the head pod |  |  |


#### RayCluster



RayCluster is the Schema for the RayClusters API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1` | | |
| `kind` _string_ | `RayCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayClusterSpec](#rayclusterspec)_ | Specification of the desired behavior of the RayCluster. |  |  |


#### RayClusterSpec



RayClusterSpec defines the desired state of RayCluster



_Appears in:_
- [RayCluster](#raycluster)
- [RayJobSpec](#rayjobspec)
- [RayServiceSpec](#rayservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enableInTreeAutoscaling` _boolean_ | EnableInTreeAutoscaling indicates whether operator should create in tree autoscaling configs |  |  |
| `autoscalerOptions` _[AutoscalerOptions](#autoscaleroptions)_ | AutoscalerOptions specifies optional configuration for the Ray autoscaler. |  |  |
| `suspend` _boolean_ | Suspend indicates whether a RayCluster should be suspended.<br />A suspended RayCluster will have head pods and worker pods deleted. |  |  |
| `headServiceAnnotations` _object (keys:string, values:string)_ |  |  |  |
| `headGroupSpec` _[HeadGroupSpec](#headgroupspec)_ | HeadGroupSpec is the spec for the head pod |  |  |
| `rayVersion` _string_ | RayVersion is used to determine the command for the Kubernetes Job managed by RayJob |  |  |
| `workerGroupSpecs` _[WorkerGroupSpec](#workergroupspec) array_ | WorkerGroupSpecs are the specs for the worker pods |  |  |


#### RayJob



RayJob is the Schema for the rayjobs API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1` | | |
| `kind` _string_ | `RayJob` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayJobSpec](#rayjobspec)_ |  |  |  |


#### RayJobSpec



RayJobSpec defines the desired state of RayJob



_Appears in:_
- [RayJob](#rayjob)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `submitterPodTemplate` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | SubmitterPodTemplate is the template for the pod that will run `ray job submit`. |  |  |
| `metadata` _object (keys:string, values:string)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `rayClusterSpec` _[RayClusterSpec](#rayclusterspec)_ | RayClusterSpec is the cluster template to run the job |  |  |
| `clusterSelector` _object (keys:string, values:string)_ | ClusterSelector is used to select running rayclusters by labels |  |  |
| `entrypoint` _string_ | Entrypoint represents the command to start execution. |  |  |
| `runtimeEnvYAML` _string_ | RuntimeEnvYAML represents the runtime environment configuration<br />provided as a multi-line YAML string. |  |  |
| `jobId` _string_ | If jobId is not set, a new jobId will be auto-generated. |  |  |
| `entrypointResources` _string_ | EntrypointResources specifies the custom resources and quantities to reserve for the<br />entrypoint command. |  |  |
| `ttlSecondsAfterFinished` _integer_ | TTLSecondsAfterFinished is the TTL to clean up RayCluster.<br />It's only working when ShutdownAfterJobFinishes set to true. | 0 |  |
| `entrypointNumCpus` _float_ | EntrypointNumCpus specifies the number of cpus to reserve for the entrypoint command. |  |  |
| `entrypointNumGpus` _float_ | EntrypointNumGpus specifies the number of gpus to reserve for the entrypoint command. |  |  |
| `shutdownAfterJobFinishes` _boolean_ | ShutdownAfterJobFinishes will determine whether to delete the ray cluster once rayJob succeed or failed. |  |  |
| `suspend` _boolean_ | Suspend specifies whether the RayJob controller should create a RayCluster instance<br />If a job is applied with the suspend field set to true,<br />the RayCluster will not be created and will wait for the transition to false.<br />If the RayCluster is already created, it will be deleted.<br />In case of transition to false a new RayCluster will be created. |  |  |




#### RayService



RayService is the Schema for the rayservices API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `ray.io/v1alpha1` | | |
| `kind` _string_ | `RayService` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RayServiceSpec](#rayservicespec)_ |  |  |  |


#### RayServiceSpec



RayServiceSpec defines the desired state of RayService



_Appears in:_
- [RayService](#rayservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serveService` _[Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core)_ | ServeService is the Kubernetes service for head node and worker nodes who have healthy http proxy to serve traffics. |  |  |
| `serviceUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |  |  |
| `deploymentUnhealthySecondThreshold` _integer_ | Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685 |  |  |
| `serveConfigV2` _string_ | Important: Run "make" to regenerate code after modifying this file<br />Defines the applications and deployments to deploy, should be a YAML multi-line scalar string. |  |  |
| `rayClusterConfig` _[RayClusterSpec](#rayclusterspec)_ |  |  |  |




#### ScaleStrategy



ScaleStrategy to remove workers



_Appears in:_
- [WorkerGroupSpec](#workergroupspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `workersToDelete` _string array_ | WorkersToDelete workers to be deleted |  |  |


#### UpscalingMode

_Underlying type:_ _string_



_Validation:_
- Enum: [Default Aggressive Conservative]

_Appears in:_
- [AutoscalerOptions](#autoscaleroptions)



#### WorkerGroupSpec



WorkerGroupSpec are the specs for the worker pods



_Appears in:_
- [RayClusterSpec](#rayclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `groupName` _string_ | we can have multiple worker groups, we distinguish them by name |  |  |
| `replicas` _integer_ | Replicas is the number of desired Pods for this worker group. See https://github.com/ray-project/kuberay/pull/1443 for more details about the reason for making this field optional. | 0 |  |
| `minReplicas` _integer_ | MinReplicas denotes the minimum number of desired Pods for this worker group. | 0 |  |
| `maxReplicas` _integer_ | MaxReplicas denotes the maximum number of desired Pods for this worker group, and the default value is maxInt32. | 2147483647 |  |
| `rayStartParams` _object (keys:string, values:string)_ | RayStartParams are the params of the start command: address, object-store-memory, ... |  |  |
| `template` _[PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podtemplatespec-v1-core)_ | Template is a pod template for the worker |  |  |
| `scaleStrategy` _[ScaleStrategy](#scalestrategy)_ | ScaleStrategy defines which pods to remove |  |  |



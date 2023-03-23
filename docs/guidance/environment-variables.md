# Network-related environment variables 

## CLUSTER_DOMAIN
- **Default**: `cluster.local`
- **Configurable**: `true`
- **Location**: KubeRay operator
- **Description**: The cluster domain name of the Kubernetes cluster in which the KubeRay operator is installed.   
It is for facilitating communication and DNS-based service discovery within a Kubernetes cluster.   
It forms part of Fully Qualified Domain Names (FQDNs) used for addressing services and resources.  
See [kuberay/pull/951](https://github.com/ray-project/kuberay/pull/951).

## FQ_RAY_IP
- **Default**: `${HEAD_SERVICE}.${NAMESPACE}.svc.${CLUSTER_DOMAIN}`
- **Configurable**: `false`
- **Location**: Worker pods only
- **Description**: Fully qualified domain name(FQDNs) of head service.  
It is used to access head service within a Kubernetes cluster. This variable is set only in worker pods, while users can utilize 'localhost' when inside the head pod. 
See [kuberay/pull/938](https://github.com/ray-project/kuberay/pull/938).

## RAY_IP
- **Default**: `${HEAD_SERVICE}`
- **Configurable**: `false`
- **Location**: Worker pods only
- **Description**: Name of head service.   
It is used to access head service within **the same namespace** in a Kubernetes cluster. This variable is deprecated and should be maintained solely for backward compatibility purposes. Instead, users are advised to use FQ_RAY_IP for this purpose.
See [kuberay/pull/938](https://github.com/ray-project/kuberay/pull/938).

## RAY_PORT
- **Default**: `6379`
- **Configurable**: `false`
- **Location**: Head pod and Worker pods
- **Description**: GCS server port for Ray >= 1.11.0. Redis port for Ray < 1.11.0.     
It is for connecting to the Ray cluster by worker nodes and drivers started within the cluster. Users should avoid setting or overriding this variable directly. Instead, they should customize it using the 'rayStartParams' in the 'headGroupSpec' (e.g., rayStartParams["port"]).
See [kuberay/pull/388](https://github.com/ray-project/kuberay/pull/388).

## RAY_ADDRESS
- **Default**: `${LOCAL_HOST}:${RAY_PORT}` for head pod. `${FQ_RAY_IP}:${RAY_PORT}` for worker pod.
- **Configurable**: `false`
- **Location**: Head pod and Worker pods
- **Description**: Address of GCS server for Ray >= 1.11.0. Address of Redis port for Ray < 1.11.0.     
It is used to connect to Ray using ray.init() when connecting from within the cluster.  
See [kuberay/pull/388](https://github.com/ray-project/kuberay/pull/388).


# GCS FT related environment variables

## RAY_REDIS_ADDRESS
- **Default**: `Not Exist`
- **Configurable**: `true`
- **Location**: Head pod only
- **Description**: Address of external redis cluster.     
It is used to configure an external Redis cluster as the backend storage,  forcing Ray to utilize the external Redis instance.  
See [GCS FT Documentation](https://github.com/ray-project/kuberay/blob/3a2be0ba85532f8cea3f8acf1f66d78bf9a47390/docs/guidance/gcs-ft.md).


# Other environment variables

## RAY_CLUSTER_NAME
- **Default**: `metadata.name` in RayCluster Custom Resource (CR)
- **Configurable**: `false`
- **Location**: Head pod and Worker pods
- **Description**: The name of the Ray cluster to which the pod belongs, as defined by 'metadata.name' in the RayCluster Custom Resource (CR).   
It is for identifying the Ray cluster to which the pod belongs.
- See [kuberay/pull/934](https://github.com/ray-project/kuberay/pull/934).

## RAY_USAGE_STATS_KUBERAY_IN_USE:
- **Default**: `1`
- **Configurable**: `false`
- **Location**: Head pod and Worker pods
- **Description**: Idenfier to identify a Ray pod that has been deployed using KubeRay.  
It is for the purpose of statistics collection only.  
See [kuberay/pull/562](https://github.com/ray-project/kuberay/pull/562).

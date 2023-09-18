# KubeRay APIServer

The KubeRay APIServer provides gRPC and HTTP APIs to manage KubeRay resources.

## Note

```text
The KubeRay APIServer is an optional component. It provides a layer of simplified configuration for KubeRay resources. The KubeRay API server is used internally by some organizations to back user interfaces for KubeRay resource management.

The KubeRay APIServer is community-managed and is not officially endorsed by the Ray maintainers. At this time, the only officially supported methods for
managing KubeRay resources are:

- Direct management of KubeRay custom resources via kubectl, kustomize, and Kubernetes language clients.
- Helm charts.

KubeRay APIServer maintainer contacts (GitHub handles):
@Jeffwan @scarlet25151
```

## Installation

### Helm

Make sure the version of Helm is v3+. Currently, [existing CI tests](https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml) are based on Helm v3.4.1 and v3.9.4.

```sh
helm version
```

### Install KubeRay Operator

* Install a stable version via Helm repository (only supports KubeRay v0.4.0+)

  ```sh
    # Install the KubeRay helm repo
    helm repo add kuberay https://ray-project.github.io/kuberay-helm/

    # Install KubeRay Operator v0.6.0.
    helm install kuberay-operator kuberay/kuberay-operator --version 0.6.0

    # Check the KubeRay Operator Pod in `default` namespace
    kubectl get pods
    # NAME                                             READY   STATUS    RESTARTS   AGE
    # kuberay-operator-7456c6b69b-t6pt7                1/1     Running   0          172m

  ```

### Install KubeRay APIServer

* Install a stable version via Helm repository (only supports KubeRay v0.4.0+)
  
  ```sh
  # Install the KubeRay helm repo
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/

  # Install KubeRay APIServer v0.6.0.
  helm install -n ray-system kuberay-apiserver kuberay/kuberay-apiserver --version 0.6.0

  # Check the KubeRay APIServer Pod in `default` namespace
  kubectl get pods
  # NAME                                             READY   STATUS    RESTARTS   AGE
  # kuberay-apiserver-857869f665-b94px               1/1     Running   0          86m
  # kuberay-operator-7456c6b69b-t6pt7                1/1     Running   0          172m
  ```

* Install the nightly version
  
  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Move to `helm-chart/kuberay-apiserver`
  cd helm-chart/kuberay-apiserver

  # Step3: Install KubeRay APIServer
  helm install kuberay-apiserver .
  ```

* Install the current (working branch) version
  
  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Change directory to the api server
  cd apiserver

  # Step3: Build docker image, create a local kind cluster and deploy api server (using helm)
  make docker-image cluster load-image deploy
  ```  

### List the chart

To list the deployments:

```sh
helm ls
# NAME                    NAMESPACE       REVISION        UPDATED                                 STATUS          CHART                   APP VERSION
# kuberay-apiserver       default         1               2023-09-15 11:34:33.895054 +0300 EEST   deployed        #kuberay-apiserver-0.6.0            
# kuberay-operator        default         1               2023-09-15 10:08:44.637539 +0300 EEST   deployed        kuberay-operator-0.6.0
```

### Uninstall the Chart

```sh
# Uninstall the `kuberay-apiserver` release
helm uninstall kuberay-apiserver

# The KubeRay APIServer Pod should be removed.
kubectl get pods
# No resources found in default namespace.
```

## Usage

After the deployment we may use the `{{baseUrl}}` to access the

* (default) for nodeport access, we provide the default http port `31888` for connection and you can connect to the server using it

* for ingress access, you will need to create your own ingress

The requests parameters detail can be seen in [KubeRay swagger](https://github.com/ray-project/kuberay/tree/master/proto/swagger), here we only present some basic example:

### Setup a smoke test

The following steps allow you to validate that the KubeRay API Server components and KubeRay Operator integrate in your environment.

1. (Optional) You may use your local kind cluster or minikube

    ```bash
    cat <<EOF | kind create cluster --name ray-test --config -
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
      kubeadmConfigPatches:
        - |
          kind: InitConfiguration
          nodeRegistration:
            kubeletExtraArgs:
              node-labels: "ingress-ready=true"
      extraPortMappings:
      - containerPort: 30265
        hostPort: 8265
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 30001
        hostPort: 10001
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 8000
        hostPort: 8000
        listenAddress: "0.0.0.0"
      - containerPort: 31888
        hostPort: 31888
        listenAddress: "0.0.0.0"
      - containerPort: 31887 
        hostPort: 31887 
        listenAddress: "0.0.0.0"
    - role: worker
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
    - role: worker
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
    ```

2. Deploy the KubeRay APIServer within the same cluster of KubeRay operator

    ```sh
    helm repo add kuberay https://ray-project.github.io/kuberay-helm/
    helm -n ray-system install kuberay-apiserver kuberay/kuberay-apiserver
    ```

3. The APIServer expose service using `NodePort` by default. You can test access by your host and port, the default port is set to `31888`.

    ```sh
    curl localhost:31888
    ...
    {
      "code": 5,
      "message": "Not Found"
    }
    ```

4. You can create `RayCluster`, `RayJobs` or `RayService` by dialing the endpoints. The following is a simple example for creating the `RayService` object, follow [swagger support](https://ray-project.github.io/kuberay/components/apiserver/#swagger-support) to get the complete definitions of APIs.

    Please note that:

    * The following examples use the `ray-system` namespace. If not already created by using the helm install steps above, you can create it prior to executing the curl examples by running `kubectl create namespace ray-system`
    * The examples assume that the cluster has at least 2 CPUs available and 4 GB of free memory. You can either increase the CPUs available to your cluster (docker settings) or reduce the CPU request in the `compute_templates` request.
    * If you are running the service and the kuberay operator on Apple Silicon Machine, you might want to use the `rayproject/ray:2.6.3-aarch64`image.

    ```sh
    # Create a template
    curl --silent -X POST 'localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates' \
    --header 'Content-Type: application/json' \
    --data '{
      "name": "default-template",
      "namespace": "ray-system",
      "cpu": 2,
      "memory": 4
    }'

    # Create the "Fruit Stand" Ray Serve example (V1 Config spec)
    curl --silent -X POST 'localhost:31888/apis/v1alpha2/namespaces/ray-system/services' \
    --header 'Content-Type: application/json' \
    --data '{
      "name": "test-v1",
      "namespace": "ray-system",
      "user": "user",
      "serviceUnhealthySecondThreshold": 900,
      "deploymentUnhealthySecondThreshold": 300,
      "serveDeploymentGraphSpec": {
        "importPath": "fruit.deployment_graph",
        "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
        "serveConfigs": [
          {
            "deploymentName": "OrangeStand",
            "replicas": 1,
            "userConfig": "price: 2",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "PearStand",
            "replicas": 1,
            "userConfig": "price: 1",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "FruitMarket",
            "replicas": 1,
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "DAGDriver",
            "replicas": 1,
            "routePrefix": "/",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          }
        ]
      },
      "clusterSpec": {
        "headGroupSpec": {
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310",
          "serviceType": "NodePort",
          "rayStartParams": {
            "dashboard-host": "0.0.0.0",
            "metrics-export-port": "8080"
          },
          "volumes": []
        },
        "workerGroupSpec": [
          {
            "groupName": "small-wg",
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3-py310",
            "replicas": 1,
            "minReplicas": 0,
            "maxReplicas": 5,
            "rayStartParams": {
              "node-ip-address": "$MY_POD_IP"
            }
          }
        ]
      }
    }'

    # Get the pods running in the namespace
    kubectl get pods -n ray-system
    # NAME                                             READY   STATUS    RESTARTS   AGE
    # test-v1-raycluster-7c2n7-head-5qpwj              1/1     Running   0          14m
    # test-v1-raycluster-7c2n7-worker-small-wg-9dbdk   1/1     Running   0          14m

    # Delete the RayService in the namespace
    curl --silent -X 'DELETE' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services/test-v1' \
    -H 'accept: application/json'
    ```

## Swagger Support

Kuberay API server has support for Swagger UI and can be reached at [localhost:31888/swagger-ui](localhost:31888/swagger-ui) for local deployments or for nodeport deployments `<host name>:31888/swagger-ui`

## Full definition endpoints

### Compute Template

For the purpose to simplify the setting of resource, we abstract the resource of the pods template resource to the `compute template` for usage, you can define the resource in the `compute template` and then choose the appropriate template for your `head` and `workergroup` when you are creating the real objects of `RayCluster`, `RayJobs` or `RayService`.

#### Create compute templates in a given namespace

```text
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

Examples:

* Request

  ```sh
  curl --silent -X 'POST' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
        "name": "default-template",
        "namespace": "ray-system",
        "cpu": 2,
        "memory": 4
      }'
  ```

* Response

  ```json
  {
    "name": "default-template",
    "namespace": "ray-system",
    "cpu": 2,
    "memory": 4
  }
  ```

#### List all compute templates in a given namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates' \
    -H 'accept: application/json'
  ```

* Response

  ```json
  {
    "computeTemplates": [
      {
        "name": "default-template",
        "namespace": "ray-system",
        "cpu": 2,
        "memory": 4
      }
    ]
  }
  ```

#### List all compute templates in all namespaces

```text
GET {{baseUrl}}/apis/v1alpha2/compute_templates
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/compute_templates' \
  -H 'accept: application/json'
  ```

* Response

    ```json
    {
      "computeTemplates": [
        {
          "name": "default-template",
          "namespace": "default",
          "cpu": 2,
          "memory": 4
        },
        {
          "name": "default-template",
          "namespace": "ray-system",
          "cpu": 2,
          "memory": 4
        }
      ]
    }
    ```

#### Get compute template by name

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates/<compute_template_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates/default-template' \
  -H 'accept: application/json'
  ```

* Response:

  ```json
  {
    "name": "default-template",
    "namespace": "ray-system",
    "cpu": 2,
    "memory": 4
  }
  ```

#### Delete compute template by name

```text
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates/<compute_template_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'DELETE' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates/default-template' \
  -H 'accept: application/json'
  ```

* Response

  ```json
  {}
  ```

### Clusters

#### Create cluster in a given namespace

```text
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters
```

Examples:

* Request

  ```sh
  curl --silent -X 'POST' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/clusters' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "test-cluster",
    "namespace": "ray-system",
    "user": "3cpo",
    "version": "2.6.3",
    "environment": "DEV",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        },
        "volumes": []
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "replicas": 2,
          "minReplicas": 0,
          "maxReplicas": 5,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    }
  }'
  ```

* Response

  ```json
  {
    "name": "test-cluster",
    "namespace": "ray-system",
    "user": "3cpo",
    "version": "2.6.3",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "replicas": 2,
          "minReplicas": 5,
          "maxReplicas": 2,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    },
    "annotations": {
      "ray.io/creation-timestamp": "2023-09-14 12:31:58.070884 +0000 UTC"
    },
    "createdAt": "2023-09-14T12:31:58Z"
  }
  ```

#### List all clusters in a given namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/clusters' \
    -H 'accept: application/json'
  ```

* Response

  ```json
  {
    "clusters": [
      {
        "name": "test-cluster",
        "namespace": "ray-system",
        "user": "3cpo",
        "version": "2.6.3",
        "clusterSpec": {
          "headGroupSpec": {
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3",
            "serviceType": "NodePort",
            "rayStartParams": {
              "dashboard-host": "0.0.0.0",
              "metrics-export-port": "8080"
            }
          },
          "workerGroupSpec": [
            {
              "groupName": "small-wg",
              "computeTemplate": "default-template",
              "image": "rayproject/ray:2.6.3",
              "replicas": 2,
              "minReplicas": 5,
              "maxReplicas": 2,
              "rayStartParams": {
                "node-ip-address": "$MY_POD_IP"
              }
            }
          ]
        },
        "annotations": {
          "ray.io/creation-timestamp": "2023-09-15 11:31:08.304211723 +0000 UTC"
        },
        "createdAt": "2023-09-15T11:31:08Z",
        "clusterState": "ready",
        "events": [
          {
            "id": "test-cluster.17850f20cdb520b5",
            "name": "test-cluster-test-cluster.17850f20cdb520b5",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created service test-cluster-head-svc",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-cluster.17850f20d04c4601",
            "name": "test-cluster-test-cluster.17850f20d04c4601",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created head pod test-cluster-head-v4dmh",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-cluster.17850f20d642aa31",
            "name": "test-cluster-test-cluster.17850f20d642aa31",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created worker pod ",
            "type": "Normal",
            "count": 2
          }
        ],
        "serviceEndpoint": {
          "dashboard": "30529",
          "head": "32208",
          "metrics": "32623",
          "redis": "30105"
        }
      }
    ]
  }   
  ```

#### List all clusters in all namespaces

```text
GET {{baseUrl}}/apis/v1alpha2/clusters
```

Examples:

* Request

  ```sh
  curl -X 'GET' \
    'http://localhost:31888/apis/v1alpha2/clusters' \
    -H 'accept: application/json'
  ```

* Response

  ```json
  {
    "clusters": [
      {
        "name": "test-cluster",
        "namespace": "ray-system",
        "user": "3cpo",
        "version": "2.6.3",
        "clusterSpec": {
          "headGroupSpec": {
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3",
            "serviceType": "NodePort",
            "rayStartParams": {
              "dashboard-host": "0.0.0.0",
              "metrics-export-port": "8080"
            }
          },
          "workerGroupSpec": [
            {
              "groupName": "small-wg",
              "computeTemplate": "default-template",
              "image": "rayproject/ray:2.6.3",
              "replicas": 2,
              "minReplicas": 5,
              "maxReplicas": 2,
              "rayStartParams": {
                "node-ip-address": "$MY_POD_IP"
              }
            }
          ]
        },
        "annotations": {
          "ray.io/creation-timestamp": "2023-09-15 11:31:08.304211723 +0000 UTC"
        },
        "createdAt": "2023-09-15T11:31:08Z",
        "clusterState": "ready",
        "events": [
          {
            "id": "test-cluster.17850f20cdb520b5",
            "name": "test-cluster-test-cluster.17850f20cdb520b5",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created service test-cluster-head-svc",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-cluster.17850f20d04c4601",
            "name": "test-cluster-test-cluster.17850f20d04c4601",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created head pod test-cluster-head-v4dmh",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-cluster.17850f20d642aa31",
            "name": "test-cluster-test-cluster.17850f20d642aa31",
            "createdAt": "2023-09-15T11:31:08Z",
            "firstTimestamp": "2023-09-15T11:31:08Z",
            "lastTimestamp": "2023-09-15T11:31:08Z",
            "reason": "Created",
            "message": "Created worker pod ",
            "type": "Normal",
            "count": 2
          }
        ],
        "serviceEndpoint": {
          "dashboard": "30529",
          "head": "32208",
          "metrics": "32623",
          "redis": "30105"
        }
      }
    ]
  }
  ```

#### Get cluster by its name and namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters/<cluster_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/clusters' \
    -H 'accept: application/json'
  ```

* Response

  ```json
  {
    "name": "test-cluster",
    "namespace": "ray-system",
    "user": "3cpo",
    "version": "2.6.3",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "replicas": 2,
          "minReplicas": 5,
          "maxReplicas": 2,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    },
    "createdAt": "2023-09-06T11:42:03Z",
    "events": [
      {
        "id": "test-cluster.17824c260cc502ff",
        "name": "test-cluster-test-cluster.17824c260cc502ff",
        "createdAt": "2023-09-06T11:35:36Z",
        "firstTimestamp": "2023-09-06T11:35:36Z",
        "lastTimestamp": "2023-09-06T11:35:36Z",
        "reason": "Created",
        "message": "Created service test-cluster-head-svc",
        "type": "Normal",
        "count": 1
      },
      {
        "id": "test-cluster.17824c26117bd44f",
        "name": "test-cluster-test-cluster.17824c26117bd44f",
        "createdAt": "2023-09-06T11:35:36Z",
        "firstTimestamp": "2023-09-06T11:35:36Z",
        "lastTimestamp": "2023-09-06T11:35:36Z",
        "reason": "Created",
        "message": "Created head pod test-cluster-head-bh75l",
        "type": "Normal",
        "count": 1
      },
      {
        "id": "test-cluster.17824c261783b274",
        "name": "test-cluster-test-cluster.17824c261783b274",
        "createdAt": "2023-09-06T11:35:36Z",
        "firstTimestamp": "2023-09-06T11:35:36Z",
        "lastTimestamp": "2023-09-06T11:35:36Z",
        "reason": "Created",
        "message": "Created worker pod ",
        "type": "Normal",
        "count": 2
      },
      {
        "id": "test-cluster.17824c8032b97213",
        "name": "test-cluster-test-cluster.17824c8032b97213",
        "createdAt": "2023-09-06T11:42:03Z",
        "firstTimestamp": "2023-09-06T11:42:03Z",
        "lastTimestamp": "2023-09-06T11:42:03Z",
        "reason": "Created",
        "message": "Created service test-cluster-head-svc",
        "type": "Normal",
        "count": 1
      },
      {
        "id": "test-cluster.17824c8033d66898",
        "name": "test-cluster-test-cluster.17824c8033d66898",
        "createdAt": "2023-09-06T11:42:03Z",
        "firstTimestamp": "2023-09-06T11:42:03Z",
        "lastTimestamp": "2023-09-06T11:42:03Z",
        "reason": "Created",
        "message": "Created head pod test-cluster-head-m4nng",
        "type": "Normal",
        "count": 1
      },
      {
        "id": "test-cluster.17824c803897ec83",
        "name": "test-cluster-test-cluster.17824c803897ec83",
        "createdAt": "2023-09-06T11:42:03Z",
        "firstTimestamp": "2023-09-06T11:42:03Z",
        "lastTimestamp": "2023-09-06T11:42:03Z",
        "reason": "Created",
        "message": "Created worker pod ",
        "type": "Normal",
        "count": 2
      }
    ],
    "serviceEndpoint": {
      "dashboard": "32348",
      "head": "32349",
      "metrics": "32134",
      "redis": "30092"
    }
  }
  ```

#### Delete cluster by its name and namespace

```text
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters/<cluster_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'DELETE' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/clusters/test-cluster' \
  -H 'accept: application/json'
  ```

* Response:

  ```json
  {}
  ```

### RayJob

#### Create ray job in a given namespace

```text
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs
```

Examples:

* Request
  
  ```sh
  curl --silent -X 'POST' \
      'http://localhost:31888/apis/v1alpha2/jobs?namespace=ray-system' \
      -H 'accept: application/json' \
      -H 'Content-Type: application/json' \
      -d '{
      "name": "rayjob-test",
      "namespace": "ray-system",
      "user": "3cp0",
      "entrypoint": "python -V",
      "clusterSpec": {
        "headGroupSpec": {
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "serviceType": "NodePort",
          "rayStartParams": {
            "dashboard-host": "0.0.0.0"
          }
        },
        "workerGroupSpec": [
          {
            "groupName": "small-wg",
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3",
            "replicas": 1,
            "minReplicas": 0,
            "maxReplicas": 1,
            "rayStartParams": {
              "metrics-export-port": "8080"
            }
          }
        ]
      }
    }
    '
  ```

* Response

  ```json
  {
    "name": "rayjob-test",
    "namespace": "ray-system",
    "user": "3cp0",
    "entrypoint": "python -V",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "replicas": 1,
          "minReplicas": 1,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          }
        }
      ]
    },
    "createdAt": "2023-09-18T07:13:34Z"
  }
  ```

#### List all jobs in a given namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/jobs' \
  -H 'accept: application/json'
  ```

* Response

  ```json
  {
    "jobs": [
      {
        "name": "rayjob-test",
        "namespace": "ray-system",
        "user": "3cp0",
        "entrypoint": "python -V",
        "jobId": "rayjob-test-k58tz",
        "clusterSpec": {
          "headGroupSpec": {
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3",
            "serviceType": "NodePort",
            "rayStartParams": {
              "dashboard-host": "0.0.0.0"
            }
          },
          "workerGroupSpec": [
            {
              "groupName": "small-wg",
              "computeTemplate": "default-template",
              "image": "rayproject/ray:2.6.3",
              "replicas": 1,
              "minReplicas": 1,
              "maxReplicas": 1,
              "rayStartParams": {
                "metrics-export-port": "8080"
              }
            }
          ]
        },
        "createdAt": "2023-09-18T07:13:34Z",
        "jobStatus": "SUCCEEDED",
        "jobDeploymentStatus": "Running",
        "message": "Job finished successfully."
      }
    ]
  }
  ```

#### List all jobs in all namespaces

```text
GET {{baseUrl}}/apis/v1alpha2/jobs
```

Examples:

* Request:
  
  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/jobs' \
  -H 'accept: application/json'
  
  ```

* Response

  ```json
  {
    "jobs": [
      {
        "name": "rayjob-test",
        "namespace": "ray-system",
        "user": "3cp0",
        "entrypoint": "python -V",
        "jobId": "rayjob-test-k58tz",
        "clusterSpec": {
          "headGroupSpec": {
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3",
            "serviceType": "NodePort",
            "rayStartParams": {
              "dashboard-host": "0.0.0.0"
            }
          },
          "workerGroupSpec": [
            {
              "groupName": "small-wg",
              "computeTemplate": "default-template",
              "image": "rayproject/ray:2.6.3",
              "replicas": 1,
              "minReplicas": 1,
              "maxReplicas": 1,
              "rayStartParams": {
                "metrics-export-port": "8080"
              }
            }
          ]
        },
        "createdAt": "2023-09-18T07:13:34Z",
        "jobStatus": "SUCCEEDED",
        "jobDeploymentStatus": "Running",
        "message": "Job finished successfully."
      }
    ]
  } 
  ```

#### Get job by its name and namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs/<job_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/jobs/rayjob-test' \
  -H 'accept: application/json'
  ```

* Response:

  ```json
  {
    "name": "rayjob-test",
    "namespace": "ray-system",
    "user": "3cp0",
    "entrypoint": "python -V",
    "jobId": "rayjob-test-k58tz",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3",
          "replicas": 1,
          "minReplicas": 1,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          }
        }
      ]
    },
    "createdAt": "2023-09-18T07:13:34Z",
    "jobStatus": "SUCCEEDED",
    "jobDeploymentStatus": "Running",
    "message": "Job finished successfully."
  }
  ```

#### Delete job by its name and namespace

```text
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs/<job_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'DELETE' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/jobs/rayjob-test' \
  -H 'accept: application/json'
  ```

* Response
  
  ```json
  {}
  ```

### RayService

#### Create ray service in a given namespace

```text
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services
```

Examples:

* Request (V1)
  
  ```sh
  curl --silent -X 'POST' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services' \
  -H 'accept: application/json' \
  -H 'Content-Type: application/json' \
  -d '{
      "name": "test-v1",
      "namespace": "ray-system",
      "user": "user",
      "serviceUnhealthySecondThreshold": 900,
      "deploymentUnhealthySecondThreshold": 300,
      "serveDeploymentGraphSpec": {
        "importPath": "fruit.deployment_graph",
        "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
        "serveConfigs": [
          {
            "deploymentName": "OrangeStand",
            "replicas": 1,
            "userConfig": "price: 2",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "PearStand",
            "replicas": 1,
            "userConfig": "price: 1",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "FruitMarket",
            "replicas": 1,
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "DAGDriver",
            "replicas": 1,
            "routePrefix": "/",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          }
        ]
      },
      "clusterSpec": {
        "headGroupSpec": {
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310",
          "serviceType": "NodePort",
          "rayStartParams": {
            "dashboard-host": "0.0.0.0",
            "metrics-export-port": "8080"
          },
          "volumes": []
        },
        "workerGroupSpec": [
          {
            "groupName": "small-wg",
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3-py310",
            "replicas": 1,
            "minReplicas": 0,
            "maxReplicas": 5,
            "rayStartParams": {
              "node-ip-address": "$MY_POD_IP"
            }
          }
        ]
      }
    }'
  ```

* Response (V1)

  ```json
    {
    "name": "test-v1",
    "namespace": "ray-system",
    "user": "user",
    "serveDeploymentGraphSpec": {
      "importPath": "fruit.deployment_graph",
      "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
      "serveConfigs": [
        {
          "deploymentName": "OrangeStand",
          "replicas": 1,
          "userConfig": "price: 2",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "PearStand",
          "replicas": 1,
          "userConfig": "price: 1",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "FruitMarket",
          "replicas": 1,
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "DAGDriver",
          "replicas": 1,
          "routePrefix": "/",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        }
      ]
    },
    "serviceUnhealthySecondThreshold": 900,
    "deploymentUnhealthySecondThreshold": 300,
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3-py310",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310",
          "replicas": 1,
          "minReplicas": 5,
          "maxReplicas": 1,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    },
    "rayServiceStatus": {},
    "createdAt": "2023-09-18T07:55:23Z",
    "deleteAt": "1969-12-31T23:59:59Z"
  }
  ```

* Request (V2)  

  ```sh
    curl -X 'POST' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '
  {
    "name": "test-v2",
    "namespace": "ray-system",
    "user": "user",
    "serviceUnhealthySecondThreshold": 900,
    "deploymentUnhealthySecondThreshold": 300,
    "serveConfigV2": "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3-py310-aarch64",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        },
        "volumes": []
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310-aarch64",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 5,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    }
  }'
  ```

* Response (V2)

  ```json
  {
    "name": "test-v2",
    "namespace": "ray-system",
    "user": "user",
    "serveConfigV2": "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n",
    "serviceUnhealthySecondThreshold": 900,
    "deploymentUnhealthySecondThreshold": 300,
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3-py310-aarch64",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310-aarch64",
          "replicas": 1,
          "minReplicas": 5,
          "maxReplicas": 1,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    },
    "rayServiceStatus": {
      "rayServiceEvents": [
        {
          "id": "test-v2.1785f3479cd90185",
          "name": "test-v2-test-v2.1785f3479cd90185",
          "createdAt": "2023-09-18T09:12:03Z",
          "firstTimestamp": "2023-09-18T09:12:03Z",
          "lastTimestamp": "2023-09-18T09:12:35Z",
          "reason": "ServiceUnhealthy",
          "message": "The service is in an unhealthy state. Controller will perform a round of actions in 10s.",
          "type": "Normal",
          "count": 5
        }
      ]
    },
    "createdAt": "2023-09-18T09:13:24Z",
    "deleteAt": "1969-12-31T23:59:59Z"
  }  
  ```

#### List all services in a given namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services
```

Examples

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services' \
  -H 'accept: application/json'
  ```

* Response:

  ```json
  {
    "services": [
    {
      "name": "test-v1",
      "namespace": "ray-system",
      "user": "user",
      "serveDeploymentGraphSpec": {
        "importPath": "fruit.deployment_graph",
        "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
        "serveConfigs": [
          {
            "deploymentName": "OrangeStand",
            "replicas": 1,
            "userConfig": "price: 2",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "PearStand",
            "replicas": 1,
            "userConfig": "price: 1",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "FruitMarket",
            "replicas": 1,
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          },
          {
            "deploymentName": "DAGDriver",
            "replicas": 1,
            "routePrefix": "/",
            "actorOptions": {
              "cpusPerActor": 0.1
            }
          }
        ]
      },
      "serviceUnhealthySecondThreshold": 900,
      "deploymentUnhealthySecondThreshold": 300,
      "clusterSpec": {
        "headGroupSpec": {
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310",
          "serviceType": "NodePort",
          "rayStartParams": {
            "dashboard-host": "0.0.0.0",
            "metrics-export-port": "8080"
          }
        },
        "workerGroupSpec": [
          {
            "groupName": "small-wg",
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3-py310",
            "replicas": 1,
            "minReplicas": 5,
            "maxReplicas": 1,
            "rayStartParams": {
              "node-ip-address": "$MY_POD_IP"
            }
          }
        ]
      },
      "rayServiceStatus": {
        "rayServiceEvents": [
          {
            "id": "test-v1.1785ef1932a53822",
            "name": "test-v1-test-v1.1785ef1932a53822",
            "createdAt": "2023-09-18T07:55:26Z",
            "firstTimestamp": "2023-09-18T07:55:26Z",
            "lastTimestamp": "2023-09-18T07:55:26Z",
            "reason": "ServiceUnhealthy",
            "message": "The service is in an unhealthy state. Controller will perform a round of actions in 10s.",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-v1.1785ef1962bfb4c2",
            "name": "test-v1-test-v1.1785ef1962bfb4c2",
            "createdAt": "2023-09-18T07:55:27Z",
            "firstTimestamp": "2023-09-18T07:55:27Z",
            "lastTimestamp": "2023-09-18T07:55:30Z",
            "reason": "WaitForServeDeploymentReady",
            "message": "Put \"http://test-v1-raycluster-vcvz2-head-svc.ray-system.svc.cluster.local:52365/api/serve/deployments/\": dial tcp 10.96.98.134:52365: connect: connection refused",
            "type": "Normal",
            "count": 3
          },
          {
            "id": "test-v1.1785ef1b0f379aa9",
            "name": "test-v1-test-v1.1785ef1b0f379aa9",
            "createdAt": "2023-09-18T07:55:34Z",
            "firstTimestamp": "2023-09-18T07:55:34Z",
            "lastTimestamp": "2023-09-18T07:55:34Z",
            "reason": "SubmittedServeDeployment",
            "message": "Controller sent API request to update Serve deployments on cluster test-v1-raycluster-vcvz2",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-v1.1785ef1bbaa5aa5f",
            "name": "test-v1-test-v1.1785ef1bbaa5aa5f",
            "createdAt": "2023-09-18T07:55:37Z",
            "firstTimestamp": "2023-09-18T07:55:37Z",
            "lastTimestamp": "2023-09-18T07:55:39Z",
            "reason": "ServiceNotReady",
            "message": "The service is not ready yet. Controller will perform a round of actions in 2s.",
            "type": "Normal",
            "count": 2
          },
          {
            "id": "test-v1.1785ef1cd4c74326",
            "name": "test-v1-test-v1.1785ef1cd4c74326",
            "createdAt": "2023-09-18T07:55:41Z",
            "firstTimestamp": "2023-09-18T07:55:41Z",
            "lastTimestamp": "2023-09-18T08:00:27Z",
            "reason": "Running",
            "message": "The Serve applicaton is now running and healthy.",
            "type": "Normal",
            "count": 136
          },
          {
            "id": "test-v1-raycluster-vcvz2.1785ef1920827324",
            "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1920827324",
            "createdAt": "2023-09-18T07:55:25Z",
            "firstTimestamp": "2023-09-18T07:55:25Z",
            "lastTimestamp": "2023-09-18T07:55:25Z",
            "reason": "Created",
            "message": "Created service test-v1-raycluster-vcvz2-head-svc",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-v1-raycluster-vcvz2.1785ef1926720c3b",
            "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1926720c3b",
            "createdAt": "2023-09-18T07:55:26Z",
            "firstTimestamp": "2023-09-18T07:55:26Z",
            "lastTimestamp": "2023-09-18T07:55:26Z",
            "reason": "Created",
            "message": "Created head pod test-v1-raycluster-vcvz2-head-959rs",
            "type": "Normal",
            "count": 1
          },
          {
            "id": "test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
            "name": "test-v1-test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
            "createdAt": "2023-09-18T07:55:26Z",
            "firstTimestamp": "2023-09-18T07:55:26Z",
            "lastTimestamp": "2023-09-18T07:55:26Z",
            "reason": "Created",
            "message": "Created worker pod ",
            "type": "Normal",
            "count": 1
          }
        ],
        "rayClusterName": "test-v1-raycluster-vcvz2",
        "serveApplicationStatus": [
          {
            "name": "default",
            "status": "RUNNING",
            "serveDeploymentStatus": [
              {
                "deploymentName": "default_DAGDriver",
                "status": "HEALTHY"
              },
              {
                "deploymentName": "default_FruitMarket",
                "status": "HEALTHY"
              },
              {
                "deploymentName": "default_MangoStand",
                "status": "HEALTHY"
              },
              {
                "deploymentName": "default_OrangeStand",
                "status": "HEALTHY"
              },
              {
                "deploymentName": "default_PearStand",
                "status": "HEALTHY"
              }
            ]
          }
        ]
      },
      "createdAt": "2023-09-18T07:55:23Z",
      "deleteAt": "1969-12-31T23:59:59Z"
    }
    ]
  }
  ```

#### List all services in all namespaces

```text
GET {{baseUrl}}/apis/v1alpha2/services
```

Examples:

* Request

  ```sh
  curl --silent -X 'GET' \
  'http://localhost:31888/apis/v1alpha2/services' \
  -H 'accept: application/json'
  ```

* Response:

  ```json
  {
    "services": [
      {
        "name": "test-v1",
        "namespace": "ray-system",
        "user": "user",
        "serveDeploymentGraphSpec": {
          "importPath": "fruit.deployment_graph",
          "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
          "serveConfigs": [
            {
              "deploymentName": "OrangeStand",
              "replicas": 1,
              "userConfig": "price: 2",
              "actorOptions": {
                "cpusPerActor": 0.1
              }
            },
            {
              "deploymentName": "PearStand",
              "replicas": 1,
              "userConfig": "price: 1",
              "actorOptions": {
                "cpusPerActor": 0.1
              }
            },
            {
              "deploymentName": "FruitMarket",
              "replicas": 1,
              "actorOptions": {
                "cpusPerActor": 0.1
              }
            },
            {
              "deploymentName": "DAGDriver",
              "replicas": 1,
              "routePrefix": "/",
              "actorOptions": {
                "cpusPerActor": 0.1
              }
            }
          ]
        },
        "serviceUnhealthySecondThreshold": 900,
        "deploymentUnhealthySecondThreshold": 300,
        "clusterSpec": {
          "headGroupSpec": {
            "computeTemplate": "default-template",
            "image": "rayproject/ray:2.6.3-py310-aarch64",
            "serviceType": "NodePort",
            "rayStartParams": {
              "dashboard-host": "0.0.0.0",
              "metrics-export-port": "8080"
            }
          },
          "workerGroupSpec": [
            {
              "groupName": "small-wg",
              "computeTemplate": "default-template",
              "image": "rayproject/ray:2.6.3-py310-aarch64",
              "replicas": 1,
              "minReplicas": 5,
              "maxReplicas": 1,
              "rayStartParams": {
                "node-ip-address": "$MY_POD_IP"
              }
            }
          ]
        },
        "rayServiceStatus": {
          "rayServiceEvents": [
            {
              "id": "test-v1.1785ef1932a53822",
              "name": "test-v1-test-v1.1785ef1932a53822",
              "createdAt": "2023-09-18T07:55:26Z",
              "firstTimestamp": "2023-09-18T07:55:26Z",
              "lastTimestamp": "2023-09-18T07:55:26Z",
              "reason": "ServiceUnhealthy",
              "message": "The service is in an unhealthy state. Controller will perform a round of actions in 10s.",
              "type": "Normal",
              "count": 1
            },
            {
              "id": "test-v1.1785ef1962bfb4c2",
              "name": "test-v1-test-v1.1785ef1962bfb4c2",
              "createdAt": "2023-09-18T07:55:27Z",
              "firstTimestamp": "2023-09-18T07:55:27Z",
              "lastTimestamp": "2023-09-18T07:55:30Z",
              "reason": "WaitForServeDeploymentReady",
              "message": "Put \"http://test-v1-raycluster-vcvz2-head-svc.ray-system.svc.cluster.local:52365/api/serve/deployments/\": dial tcp 10.96.98.134:52365: connect: connection refused",
              "type": "Normal",
              "count": 3
            },
            {
              "id": "test-v1.1785ef1b0f379aa9",
              "name": "test-v1-test-v1.1785ef1b0f379aa9",
              "createdAt": "2023-09-18T07:55:34Z",
              "firstTimestamp": "2023-09-18T07:55:34Z",
              "lastTimestamp": "2023-09-18T07:55:34Z",
              "reason": "SubmittedServeDeployment",
              "message": "Controller sent API request to update Serve deployments on cluster test-v1-raycluster-vcvz2",
              "type": "Normal",
              "count": 1
            },
            {
              "id": "test-v1.1785ef1bbaa5aa5f",
              "name": "test-v1-test-v1.1785ef1bbaa5aa5f",
              "createdAt": "2023-09-18T07:55:37Z",
              "firstTimestamp": "2023-09-18T07:55:37Z",
              "lastTimestamp": "2023-09-18T07:55:39Z",
              "reason": "ServiceNotReady",
              "message": "The service is not ready yet. Controller will perform a round of actions in 2s.",
              "type": "Normal",
              "count": 2
            },
            {
              "id": "test-v1.1785ef1cd4c74326",
              "name": "test-v1-test-v1.1785ef1cd4c74326",
              "createdAt": "2023-09-18T07:55:41Z",
              "firstTimestamp": "2023-09-18T07:55:41Z",
              "lastTimestamp": "2023-09-18T08:05:27Z",
              "reason": "Running",
              "message": "The Serve applicaton is now running and healthy.",
              "type": "Normal",
              "count": 277
            },
            {
              "id": "test-v1-raycluster-vcvz2.1785ef1920827324",
              "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1920827324",
              "createdAt": "2023-09-18T07:55:25Z",
              "firstTimestamp": "2023-09-18T07:55:25Z",
              "lastTimestamp": "2023-09-18T07:55:25Z",
              "reason": "Created",
              "message": "Created service test-v1-raycluster-vcvz2-head-svc",
              "type": "Normal",
              "count": 1
            },
            {
              "id": "test-v1-raycluster-vcvz2.1785ef1926720c3b",
              "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1926720c3b",
              "createdAt": "2023-09-18T07:55:26Z",
              "firstTimestamp": "2023-09-18T07:55:26Z",
              "lastTimestamp": "2023-09-18T07:55:26Z",
              "reason": "Created",
              "message": "Created head pod test-v1-raycluster-vcvz2-head-959rs",
              "type": "Normal",
              "count": 1
            },
            {
              "id": "test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
              "name": "test-v1-test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
              "createdAt": "2023-09-18T07:55:26Z",
              "firstTimestamp": "2023-09-18T07:55:26Z",
              "lastTimestamp": "2023-09-18T07:55:26Z",
              "reason": "Created",
              "message": "Created worker pod ",
              "type": "Normal",
              "count": 1
            }
          ],
          "rayClusterName": "test-v1-raycluster-vcvz2",
          "serveApplicationStatus": [
            {
              "name": "default",
              "status": "RUNNING",
              "serveDeploymentStatus": [
                {
                  "deploymentName": "default_FruitMarket",
                  "status": "HEALTHY"
                },
                {
                  "deploymentName": "default_MangoStand",
                  "status": "HEALTHY"
                },
                {
                  "deploymentName": "default_OrangeStand",
                  "status": "HEALTHY"
                },
                {
                  "deploymentName": "default_PearStand",
                  "status": "HEALTHY"
                },
                {
                  "deploymentName": "default_DAGDriver",
                  "status": "HEALTHY"
                }
              ]
            }
          ]
        },
        "createdAt": "2023-09-18T07:55:23Z",
        "deleteAt": "1969-12-31T23:59:59Z"
      }
    ]
  }
  ```

#### Get service by its name and namespace

```text
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services/<service_name>
```

Examples:

* Request:

  ```sh
  curl --silent -X 'GET' \
    'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services/test-v1' \
    -H 'accept: application/json'  
  ```

* Response:

  ```json
  {
    "name": "test-v1",
    "namespace": "ray-system",
    "user": "user",
    "serveDeploymentGraphSpec": {
      "importPath": "fruit.deployment_graph",
      "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
      "serveConfigs": [
        {
          "deploymentName": "OrangeStand",
          "replicas": 1,
          "userConfig": "price: 2",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "PearStand",
          "replicas": 1,
          "userConfig": "price: 1",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "FruitMarket",
          "replicas": 1,
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        },
        {
          "deploymentName": "DAGDriver",
          "replicas": 1,
          "routePrefix": "/",
          "actorOptions": {
            "cpusPerActor": 0.1
          }
        }
      ]
    },
    "serviceUnhealthySecondThreshold": 900,
    "deploymentUnhealthySecondThreshold": 300,
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.6.3-py310-aarch64",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.6.3-py310-aarch64",
          "replicas": 1,
          "minReplicas": 5,
          "maxReplicas": 1,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          }
        }
      ]
    },
    "rayServiceStatus": {
      "rayServiceEvents": [
        {
          "id": "test-v1.1785ef1932a53822",
          "name": "test-v1-test-v1.1785ef1932a53822",
          "createdAt": "2023-09-18T07:55:26Z",
          "firstTimestamp": "2023-09-18T07:55:26Z",
          "lastTimestamp": "2023-09-18T07:55:26Z",
          "reason": "ServiceUnhealthy",
          "message": "The service is in an unhealthy state. Controller will perform a round of actions in 10s.",
          "type": "Normal",
          "count": 1
        },
        {
          "id": "test-v1.1785ef1962bfb4c2",
          "name": "test-v1-test-v1.1785ef1962bfb4c2",
          "createdAt": "2023-09-18T07:55:27Z",
          "firstTimestamp": "2023-09-18T07:55:27Z",
          "lastTimestamp": "2023-09-18T07:55:30Z",
          "reason": "WaitForServeDeploymentReady",
          "message": "Put \"http://test-v1-raycluster-vcvz2-head-svc.ray-system.svc.cluster.local:52365/api/serve/deployments/\": dial tcp 10.96.98.134:52365: connect: connection refused",
          "type": "Normal",
          "count": 3
        },
        {
          "id": "test-v1.1785ef1b0f379aa9",
          "name": "test-v1-test-v1.1785ef1b0f379aa9",
          "createdAt": "2023-09-18T07:55:34Z",
          "firstTimestamp": "2023-09-18T07:55:34Z",
          "lastTimestamp": "2023-09-18T07:55:34Z",
          "reason": "SubmittedServeDeployment",
          "message": "Controller sent API request to update Serve deployments on cluster test-v1-raycluster-vcvz2",
          "type": "Normal",
          "count": 1
        },
        {
          "id": "test-v1.1785ef1bbaa5aa5f",
          "name": "test-v1-test-v1.1785ef1bbaa5aa5f",
          "createdAt": "2023-09-18T07:55:37Z",
          "firstTimestamp": "2023-09-18T07:55:37Z",
          "lastTimestamp": "2023-09-18T07:55:39Z",
          "reason": "ServiceNotReady",
          "message": "The service is not ready yet. Controller will perform a round of actions in 2s.",
          "type": "Normal",
          "count": 2
        },
        {
          "id": "test-v1.1785ef1cd4c74326",
          "name": "test-v1-test-v1.1785ef1cd4c74326",
          "createdAt": "2023-09-18T07:55:41Z",
          "firstTimestamp": "2023-09-18T07:55:41Z",
          "lastTimestamp": "2023-09-18T08:36:25Z",
          "reason": "Running",
          "message": "The Serve applicaton is now running and healthy.",
          "type": "Normal",
          "count": 837
        },
        {
          "id": "test-v1-raycluster-vcvz2.1785ef1920827324",
          "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1920827324",
          "createdAt": "2023-09-18T07:55:25Z",
          "firstTimestamp": "2023-09-18T07:55:25Z",
          "lastTimestamp": "2023-09-18T07:55:25Z",
          "reason": "Created",
          "message": "Created service test-v1-raycluster-vcvz2-head-svc",
          "type": "Normal",
          "count": 1
        },
        {
          "id": "test-v1-raycluster-vcvz2.1785ef1926720c3b",
          "name": "test-v1-test-v1-raycluster-vcvz2.1785ef1926720c3b",
          "createdAt": "2023-09-18T07:55:26Z",
          "firstTimestamp": "2023-09-18T07:55:26Z",
          "lastTimestamp": "2023-09-18T07:55:26Z",
          "reason": "Created",
          "message": "Created head pod test-v1-raycluster-vcvz2-head-959rs",
          "type": "Normal",
          "count": 1
        },
        {
          "id": "test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
          "name": "test-v1-test-v1-raycluster-vcvz2.1785ef192cb1eb9d",
          "createdAt": "2023-09-18T07:55:26Z",
          "firstTimestamp": "2023-09-18T07:55:26Z",
          "lastTimestamp": "2023-09-18T07:55:26Z",
          "reason": "Created",
          "message": "Created worker pod ",
          "type": "Normal",
          "count": 1
        }
      ],
      "rayClusterName": "test-v1-raycluster-vcvz2",
      "serveApplicationStatus": [
        {
          "name": "default",
          "status": "RUNNING",
          "serveDeploymentStatus": [
            {
              "deploymentName": "default_DAGDriver",
              "status": "HEALTHY"
            },
            {
              "deploymentName": "default_FruitMarket",
              "status": "HEALTHY"
            },
            {
              "deploymentName": "default_MangoStand",
              "status": "HEALTHY"
            },
            {
              "deploymentName": "default_OrangeStand",
              "status": "HEALTHY"
            },
            {
              "deploymentName": "default_PearStand",
              "status": "HEALTHY"
            }
          ]
        }
      ]
    },
    "createdAt": "2023-09-18T07:55:23Z",
    "deleteAt": "1969-12-31T23:59:59Z"
  }
  ```

#### Delete service by its name and namespace

```text
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services/<service_name>
```

Examples:

* Request

  ```sh
  curl --silent -X 'DELETE' \
  'http://localhost:31888/apis/v1alpha2/namespaces/ray-system/services/test-v1' \
  -H 'accept: application/json'  
  ```

* Response

  ```json
  {}
  ```

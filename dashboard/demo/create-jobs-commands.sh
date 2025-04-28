curl --silent -X 'GET' \
'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
-H 'accept: application/json'

# quick success job
curl -X 'POST' \
    'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "rayjob-test2",
    "namespace": "kubeflow-ml",
    "user": "3cp0",
    "version": "2.9.0",
    "entrypoint": "python -V",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        },
        "labels": {
          "sidecar.istio.io/inject": "false"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          },
          "labels": {
            "sidecar.istio.io/inject": "false"
          }
        }
      ]
    }
  }'

# quick failed job
curl -X 'POST' \
    'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "rayjob-test-fail",
    "namespace": "kubeflow-ml",
    "user": "3cp0",
    "version": "2.9.0",
    "entrypoint": "python -c 'fdafdafd'",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        },
        "labels": {
          "sidecar.istio.io/inject": "false"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          },
          "labels": {
            "sidecar.istio.io/inject": "false"
          }
        }
      ]
    }
  }'

# long running job
curl -X 'POST' \
    'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "rayjob-test-long-running4",
    "namespace": "kubeflow-ml",
    "user": "3cp0",
    "version": "2.9.0",
    "entrypoint": "python -c \"import time; time.sleep(100)\"",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        },
        "labels": {
          "sidecar.istio.io/inject": "false"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          },
          "labels": {
            "sidecar.istio.io/inject": "false"
          }
        }
      ]
    }
  }'

# run for 10s then fail
curl -X 'POST' \
    'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "rayjob-test-10s-then-fail",
    "namespace": "kubeflow-ml",
    "user": "3cp0",
    "version": "2.9.0",
    "entrypoint": "python -c \"import time; time.sleep(10); raise Exception(\\\"oof\\\")\"",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        },
        "labels": {
          "sidecar.istio.io/inject": "false"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          },
          "labels": {
            "sidecar.istio.io/inject": "false"
          }
        }
      ]
    }
  }'


curl --silent -X 'POST' \
  'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/compute_templates' \
  -H 'accept: application/json' \
  -H 'Content-Type: application/json' \
  -d '{
      "name": "test-template-no-istio",
      "namespace": "kubeflow-ml",
      "cpu": 2,
      "memory": 4
    }'

# more metadata
curl -X 'POST' \
    'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
    "name": "rayjob-test-long-running4",
    "namespace": "kubeflow-ml",
    "user": "3cp0",
    "version": "2.9.0",
    "entrypoint": "python -c \"import time; time.sleep(100)\"",
    "metadata": {
      "description": "test pipeline"
    },
    "shutdown_after_job_finishes": true,
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0"
        },
        "labels": {
          "sidecar.istio.io/inject": "false"
        }
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 1,
          "rayStartParams": {
            "metrics-export-port": "8080"
          },
          "labels": {
            "sidecar.istio.io/inject": "false"
          }
        }
      ]
    }
  }'
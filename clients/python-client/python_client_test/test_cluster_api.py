import unittest
import copy
import re
from python_client.utils import kuberay_cluster_utils, kuberay_cluster_builder
from python_client import kuberay_cluster_api



test_cluster_body: dict = {
    "apiVersion": "ray.io/v1alpha1",
    "kind": "RayCluster",
    "metadata": {
        "labels": {"controller-tools.k8s.io": "1.0"},
        "name": "raycluster-complete-raw",
    },
    "spec": {
        "rayVersion": "2.46.0",
        "headGroupSpec": {
            "rayStartParams": {"dashboard-host": "0.0.0.0"},
            "template": {
                "metadata": {"labels": {}},
                "spec": {
                    "containers": [
                        {
                            "name": "ray-head",
                            "image": "rayproject/ray:2.46.0",
                            "ports": [
                                {"containerPort": 6379, "name": "gcs"},
                                {"containerPort": 8265, "name": "dashboard"},
                                {"containerPort": 10001, "name": "client"},
                            ],
                            "lifecycle": {
                                "preStop": {
                                    "exec": {"command": ["/bin/sh", "-c", "ray stop"]}
                                }
                            },
                            "volumeMounts": [
                                {"mountPath": "/tmp/ray", "name": "ray-logs"}
                            ],
                            "resources": {
                                "limits": {"cpu": "1", "memory": "2G"},
                                "requests": {"cpu": "500m", "memory": "2G"},
                            },
                        }
                    ],
                    "volumes": [{"name": "ray-logs", "emptyDir": {}}],
                },
            },
        },
        "workerGroupSpecs": [
            {
                "replicas": 1,
                "minReplicas": 1,
                "maxReplicas": 10,
                "groupName": "small-group",
                "rayStartParams": {},
                "template": {
                    "spec": {
                        "containers": [
                            {
                                "name": "ray-worker",
                                "image": "rayproject/ray:2.46.0",
                                "lifecycle": {
                                    "preStop": {
                                        "exec": {
                                            "command": ["/bin/sh", "-c", "ray stop"]
                                        }
                                    }
                                },
                                "volumeMounts": [
                                    {"mountPath": "/tmp/ray", "name": "ray-logs"}
                                ],
                                "resources": {
                                    "limits": {"cpu": "1", "memory": "1G"},
                                    "requests": {"cpu": "500m", "memory": "1G"},
                                },
                            },
                            {
                                "name": "side-car",
                                "image": "rayproject/ray:2.46.0",
                                "resources": {
                                    "limits": {"cpu": "1", "memory": "1G"},
                                    "requests": {"cpu": "500m", "memory": "1G"},
                                },
                            }
                        ],
                        "volumes": [{"name": "ray-logs", "emptyDir": {}}],
                    }
                },
            }
        ],
    },
    "status": {
        "availableWorkerReplicas": 2,
        "desiredWorkerReplicas": 1,
        "endpoints": {
            "client": "10001",
            "dashboard": "8265",
            "gcs-server": "6379"
        },
        "head": {
            "serviceIP": "10.152.183.194"
        },
        "lastUpdateTime": "2023-02-16T05:15:17Z",
        "maxWorkerReplicas": 2
    }
}

class TestUtils(unittest.TestCase):
    def __init__(self, methodName: str = ...) -> None:
        super().__init__(methodName)

        self.api = kuberay_cluster_api.RayClusterApi()

        # mock the get_ray_cluster_status method
        self.api.get_ray_cluster_status = self.get_ray_cluster_status_mock


    def test_wait_until_ray_cluster_running(self):
        is_running: bool = self.api.wait_until_ray_cluster_running(name = "small-cluster", k8s_namespace = "default", timeout = 60, delay_between_attempts = 5)

        self.assertEqual(is_running, True)

    def test_wait_until_ray_cluster_running_timeout(self):
        is_running: bool = self.api.wait_until_ray_cluster_running(name = "small-cluster", k8s_namespace = "default", timeout = 60, delay_between_attempts = 5)

        self.assertEqual(is_running, True)

    # mock the get_ray_cluster_status method
    def get_ray_cluster_status_mock(self, name: str, k8s_namespace: str = "default", timeout: int = 5, delay_between_attempts: int = 5):
        return test_cluster_body["status"]

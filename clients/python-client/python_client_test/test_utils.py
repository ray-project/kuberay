import unittest
import copy
import re
from utils import kuberay_cluster_utils, kuberay_cluster_builder



test_cluster_body: dict = {
    "apiVersion": "ray.io/v1alpha1",
    "kind": "RayCluster",
    "metadata": {
        "labels": {"controller-tools.k8s.io": "1.0"},
        "name": "raycluster-complete-raw",
    },
    "spec": {
        "rayVersion": "2.2.0",
        "headGroupSpec": {
            "serviceType": "ClusterIP",
            "rayStartParams": {"dashboard-host": "0.0.0.0", "block": "true"},
            "template": {
                "metadata": {"labels": {}},
                "spec": {
                    "containers": [
                        {
                            "name": "ray-head",
                            "image": "rayproject/ray:2.2.0",
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
                "rayStartParams": {"block": "true"},
                "template": {
                    "spec": {
                        "containers": [
                            {
                                "name": "ray-worker",
                                "image": "rayproject/ray:2.2.0",
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
                                "image": "rayproject/ray:2.2.0",
                                "resources": {
                                    "limits": {"cpu": "1", "memory": "1G"},
                                    "requests": {"cpu": "500m", "memory": "1G"},
                                },
                            }
                        ],
                        "initContainers": [
                            {
                                "name": "init",
                                "image": "busybox:1.28",
                                "command": [
                                    "sh",
                                    "-c",
                                    "until nslookup $RAY_IP.$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace).svc.cluster.local; do echo waiting for K8s Service $RAY_IP; sleep 2; done",
                                ],
                            }
                        ],
                        "volumes": [{"name": "ray-logs", "emptyDir": {}}],
                    }
                },
            }
        ],
    },
}

class TestUtils(unittest.TestCase):
    def __init__(self, methodName: str = ...) -> None:
        super().__init__(methodName)
        self.director = kuberay_cluster_builder.Director()
        self.utils = kuberay_cluster_utils.ClusterUtils()

    def test_update_worker_group_replicas(self):
        cluster = self.director.build_small_cluster(name="small-cluster")

        actual = cluster["metadata"]["name"]
        expected = "small-cluster"
        self.assertEqual(actual, expected)

        cluster, succeeded = self.utils.update_worker_group_replicas(
            cluster,
            group_name="small-cluster-workers",
            max_replicas=10,
            min_replicas=1,
            replicas=5,
        )

        self.assertEqual(succeeded, True)

        # testing the workergroup
        actual = cluster["spec"]["workerGroupSpecs"][0]["replicas"]
        expected = 5
        self.assertEqual(actual, expected)

        actual = cluster["spec"]["workerGroupSpecs"][0]["maxReplicas"]
        expected = 10
        self.assertEqual(actual, expected)

        actual = cluster["spec"]["workerGroupSpecs"][0]["minReplicas"]
        expected = 1
        self.assertEqual(actual, expected)

    def test_update_worker_group_resources(self):
        cluster: dict = copy.deepcopy(test_cluster_body)
        actual = cluster["metadata"]["name"]
        expected = "raycluster-complete-raw"
        self.assertEqual(actual, expected)

        cluster, succeeded = self.utils.update_worker_group_resources(
            cluster,
            group_name="small-group",
            cpu_requests = "3",
            memory_requests = "5G",
            cpu_limits = "5",
            memory_limits = "10G",
            container_name = "unspecified", 
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][0]["template"]["spec"]["containers"][0]["resources"]["requests"]["cpu"], "3")
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][0]["template"]["spec"]["containers"][1]["resources"]["requests"]["cpu"], "500m")
        
        cluster, succeeded = self.utils.update_worker_group_resources(
            cluster,
            group_name="small-group",
            cpu_requests = "4",
            memory_requests = "5G",
            cpu_limits = "5",
            memory_limits = "10G",
            container_name = "side-car", 
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][0]["template"]["spec"]["containers"][1]["resources"]["requests"]["cpu"], "4")
        

        cluster, succeeded = self.utils.update_worker_group_resources(
            cluster,
            group_name="small-group",
            cpu_requests = "4",
            memory_requests = "15G",
            cpu_limits = "5",
            memory_limits = "25G",
            container_name = "all_containers", 
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][0]["template"]["spec"]["containers"][1]["resources"]["requests"]["memory"], "15G")
        
        cluster, succeeded = self.utils.update_worker_group_resources(
            cluster,
            group_name="small-group",
            cpu_requests = "4",
            memory_requests = "15G",
            cpu_limits = "5",
            memory_limits = "25G",
            container_name = "wrong_name", 
        )
        self.assertEqual(succeeded, False)

        # missing parameter test
        with self.assertRaises(TypeError):
            cluster, succeeded = self.utils.update_worker_group_resources(
            cluster,
            group_name="small-group",
            cpu_requests = "4", 
        )

    def test_duplicate_worker_group(self):
        cluster = self.director.build_small_cluster(name="small-cluster")
        actual = cluster["metadata"]["name"]
        expected = "small-cluster"
        self.assertEqual(actual, expected)

        cluster, succeeded = self.utils.duplicate_worker_group(
            cluster,
            group_name="small-cluster-workers",
            new_group_name="new-small-group-workers", 
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][1]["groupName"], "new-small-group-workers")
        self.assertEqual(cluster["spec"]["workerGroupSpecs"][1]["template"]["spec"]["containers"][0]["resources"]["requests"]["cpu"], "1")

        # missing parameter test
        with self.assertRaises(TypeError):
            cluster, succeeded = self.utils.duplicate_worker_group(
            cluster,
            group_name="small-cluster-workers", 
        )
        

    def test_delete_worker_group(self):
        cluster = self.director.build_small_cluster(name="small-cluster")
        actual = cluster["metadata"]["name"]
        expected = "small-cluster"
        self.assertEqual(actual, expected)

        cluster, succeeded = self.utils.delete_worker_group(
            cluster,
            group_name="small-cluster-workers",
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(len(cluster["spec"]["workerGroupSpecs"]),0)

        # deleting the same worker group again should fail
        with self.assertRaises(AssertionError):
            cluster, succeeded = self.utils.delete_worker_group(
                cluster,
                group_name="small-cluster-workers",
            )
    def test_delete_worker_group(self):
        """
        Test delete_worker_group
        """
        cluster = self.director.build_small_cluster(name="small-cluster")
        actual = cluster["metadata"]["name"]
        expected = "small-cluster"
        self.assertEqual(actual, expected)

        cluster, succeeded = self.utils.delete_worker_group(
            cluster,
            group_name="small-cluster-workers",
        )
        self.assertEqual(succeeded, True)
        self.assertEqual(len(cluster["spec"]["workerGroupSpecs"]),0)

        # deleting the same worker group again should fail
        with self.assertRaises(AssertionError):
            cluster, succeeded = self.utils.delete_worker_group(
                cluster,
                group_name="small-cluster-workers",
            )

    def test_name(self):
        self.assertEqual(self.utils.is_valid_name("name"), True)
        self.assertEqual(self.utils.is_valid_name("name-"), False)
        self.assertEqual(self.utils.is_valid_name(".name"), False)
        self.assertEqual(self.utils.is_valid_name("name_something"), False)
        self.assertEqual(self.utils.is_valid_name("toooooooooooooooooooooooooooooooooooooooooo-loooooooooooooooooooong"), False)


    def test_label(self):
        self.assertEqual(self.utils.is_valid_label("name"), True)
        self.assertEqual(self.utils.is_valid_label("name-"), False)
        self.assertEqual(self.utils.is_valid_label(".name"), False)
        self.assertEqual(self.utils.is_valid_label("name_something"), True)
        self.assertEqual(self.utils.is_valid_label("good.name"), True)
        self.assertEqual(self.utils.is_valid_label("toooooooooooooooooooooooooooooooooooooooooo-loooooooooooooooooooong"), False)
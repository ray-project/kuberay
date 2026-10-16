import threading
import unittest
from multiprocessing.pool import ThreadPool
from unittest.mock import patch

from kubernetes.client.rest import ApiException
from python_client import constants, kuberay_cluster_api


class TestListRayClusters(unittest.TestCase):
    def setUp(self):
        with patch.object(kuberay_cluster_api.config, "load_kube_config"):
            self.api = kuberay_cluster_api.RayClusterApi()
        self.addCleanup(self.api.api.api_client.close)
        self.addCleanup(self.api.core_v1_api.api_client.close)

    def test_list_ray_clusters_async(self):
        clusters = {"items": [{"metadata": {"name": "cluster"}}]}
        release = threading.Event()
        pool = ThreadPool(1)

        def list_clusters():
            release.wait()
            return clusters

        pending = pool.apply_async(list_clusters)
        try:
            with patch.object(
                self.api.api, "list_namespaced_custom_object", return_value=pending
            ) as list_objects:
                result = self.api.list_ray_clusters(
                    k8s_namespace="test", label_selector="app=ray", async_req=True
                )
                self.assertIs(result, pending)
                self.assertFalse(result.ready())
                list_objects.assert_called_once_with(
                    group=constants.GROUP,
                    version=constants.CLUSTER_VERSION,
                    plural=constants.CLUSTER_PLURAL,
                    namespace="test",
                    label_selector="app=ray",
                    async_req=True,
                )
                release.set()
                self.assertEqual(result.get(timeout=5), clusters)
        finally:
            release.set()
            pool.close()
            pool.join()

    def test_list_ray_clusters_async_error(self):
        def list_clusters():
            raise ApiException(status=404)

        pool = ThreadPool(1)
        pending = pool.apply_async(list_clusters)
        try:
            with patch.object(
                self.api.api, "list_namespaced_custom_object", return_value=pending
            ):
                result = self.api.list_ray_clusters(async_req=True)
                with self.assertRaises(ApiException) as error:
                    result.get(timeout=5)
                self.assertEqual(error.exception.status, 404)
        finally:
            pool.close()
            pool.join()

    def test_list_ray_clusters_sync(self):
        for clusters in ({"items": []}, {"items": [{"metadata": {"name": "cluster"}}]}):
            with self.subTest(clusters=clusters):
                with patch.object(
                    self.api.api, "list_namespaced_custom_object", return_value=clusters
                ):
                    self.assertEqual(self.api.list_ray_clusters(), clusters)

    def test_list_ray_clusters_sync_error(self):
        with patch.object(
            self.api.api,
            "list_namespaced_custom_object",
            side_effect=ApiException(status=404),
        ):
            self.assertIsNone(self.api.list_ray_clusters())

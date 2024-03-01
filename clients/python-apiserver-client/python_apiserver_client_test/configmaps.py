from kubernetes import client, config

CMAP_VALUE = """
import ray
import os
import requests

ray.init()

@ray.remote
class Counter:
    def __init__(self):
        # Used to verify runtimeEnv
        self.name = os.getenv("counter_name")
        assert self.name == "test_counter"
        self.counter = 0

    def inc(self):
        self.counter += 1

    def get_counter(self):
        return "{} got {}".format(self.name, self.counter)

counter = Counter.remote()

for _ in range(5):
    ray.get(counter.inc.remote())
    print(ray.get(counter.get_counter.remote()))

# Verify that the correct runtime env was used for the job.
assert requests.__version__ == "2.26.0"
"""
CMAP_NAME = "ray-job-code-sample"


class ConfigmapsManager:
    """
    Simple support class to manage config maps. Assumes local access to Kubectl
    """
    def __init__(self):
        config.load_kube_config()
        self.api_instance = client.CoreV1Api()

    def list_configmaps(self) -> list[str]:
        cm_list = self.api_instance.list_namespaced_config_map(namespace="default").items
        return [cm.metadata.name for cm in cm_list]

    def create_code_map(self) -> None:
        cmap = client.V1ConfigMap()
        cmap.metadata = client.V1ObjectMeta(name=CMAP_NAME)
        cmap.data = {"sample_code.py": CMAP_VALUE}
        self.api_instance.create_namespaced_config_map(namespace="default", body=cmap)

    def delete_configmap(self, name: str) -> None:
        self.api_instance.delete_namespaced_config_map(name=name, namespace="default")

    def cleanup(self) -> None:
        maps = self.list_configmaps()
        for m in maps:
            if m == CMAP_NAME or m.endswith("template"):
                self.delete_configmap(m)
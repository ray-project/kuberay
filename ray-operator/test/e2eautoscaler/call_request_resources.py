import ray
from ray.autoscaler.sdk import request_resources

ray.init(namespace="default_namespace")
request_resources(num_cpus=3)

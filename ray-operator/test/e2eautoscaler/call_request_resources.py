import ray
import time
from ray.autoscaler.sdk import request_resources

ray.init(namespace="default_namespace")
request_resources(num_cpus=3)
while len(ray.nodes()) != 4: # 1 head + 3 workers
    time.sleep(1)

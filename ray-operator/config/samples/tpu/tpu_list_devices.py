import ray
import jax

from jax.experimental import multihost_utils

ray.init()

@ray.remote(resources={"TPU": 4})
def tpu_cores():
    multihost_utils.sync_global_devices("sync")
    return "TPU cores:" + str(jax.device_count())

num_workers = int(ray.available_resources()["TPU"]) // 4
result = [tpu_cores.remote() for _ in range(num_workers)]
print(ray.get(result))

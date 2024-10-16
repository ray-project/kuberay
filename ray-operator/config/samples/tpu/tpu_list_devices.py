import ray
import jax

ray.init()

@ray.remote(resources={"TPU": 4})
def tpu_cores():
    return "TPU cores:" + str(jax.device_count())

num_workers = int(ray.available_resources()["TPU"])
result = [tpu_cores.remote() for _ in range(num_workers)]
print(ray.get(result))

import ray

ray.init(
    runtime_env={
        "pip": [
            "jax[tpu]==0.4.33",
            "-f https://storage.googleapis.com/jax-releases/libtpu_releases.html",
        ]
    }
)

@ray.remote(resources={"TPU": 4})
def tpu_cores():
    import jax
    return "TPU cores:" + str(jax.device_count())

num_workers = int(os.environ['TPU_HOSTS']) # Set in env of RayJob or RayCluster.
result = [tpu_cores.remote() for _ in range(num_workers)]
print(ray.get(result))

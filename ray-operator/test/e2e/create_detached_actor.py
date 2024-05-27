import ray
import sys

@ray.remote(num_cpus=1)
class Actor:
  pass

ray.init(namespace="default_namespace")
Actor.options(name=sys.argv[1], lifetime="detached").remote()

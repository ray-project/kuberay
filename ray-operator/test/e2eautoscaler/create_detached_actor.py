import ray
import sys
import argparse

parser = argparse.ArgumentParser()
parser.add_argument('name')
parser.add_argument('--num-cpus', type=float, default=1)
parser.add_argument('--num-gpus', type=float, default=0)
parser.add_argument('--num-custom-resources', type=float, default=0)
args = parser.parse_args()

@ray.remote(num_cpus=args.num_cpus, num_gpus=args.num_gpus, resources={"CustomResource": args.num_custom_resources})
class Actor:
    pass


ray.init(namespace="default_namespace")
Actor.options(name=args.name, lifetime="detached").remote()

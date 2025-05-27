import argparse
import ray
from ray.autoscaler.sdk import request_resources

parser = argparse.ArgumentParser()
parser.add_argument("--num-cpus", type=float, default=1)
parser.add_argument("--num-gpus", type=float, default=0)
parser.add_argument("--num-custom-resources", type=float, default=0)
args = parser.parse_args()

ray.init(namespace="default_namespace")

kwargs = {"num_cpus": args.num_cpus}
if args.num_gpus > 0:
    kwargs["num_gpus"] = args.num_gpus
if args.num_custom_resources > 0:
    kwargs["custom_resources"] = {"CustomResource": args.num_custom_resources}

request_resources(**kwargs)

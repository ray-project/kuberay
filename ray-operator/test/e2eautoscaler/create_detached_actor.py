import json
from typing import Optional
import ray
import argparse

parser = argparse.ArgumentParser()

class ParseKeyValueAction(argparse.Action):
    def __call__(self, parser: argparse.ArgumentParser, namespace: argparse.Namespace, values: str, option_string: Optional[str] = None):
        setattr(namespace, self.dest, dict())
        for item in values.split(","):
            try:
                key, value = item.split("=")
                try:
                    value = float(value)
                except ValueError:
                    raise argparse.ArgumentError(self, f"invalid value for key {key}, expected a float: {value}")
                getattr(namespace, self.dest)[key] = value
            except ValueError:
                raise argparse.ArgumentError(self, f"invalid key=value pair, expected format KEY1=VALUE1,KEY2=VALUE2,...: {item}")


parser.add_argument('name')
parser.add_argument('--num-cpus', type=float, default=1, help="number of CPUs")
parser.add_argument('--num-gpus', type=float, default=0, help="number of GPUs")
parser.add_argument('--custom-resources', action=ParseKeyValueAction, type=str, default={}, help="Ray custom resources as key=value pairs where value is a float, e.g. TPU=2,CustomResource=1.5", metavar="KEY1=VALUE1,KEY2=VALUE2,")
args = parser.parse_args()

# set max_restarts=-1 as a workaround to restart unexpected death in tests.
# TODO (rueian): Remove the max_restarts workaround when https://github.com/ray-project/ray/issues/40864 is fixed.
@ray.remote(max_restarts=-1, num_cpus=args.num_cpus, num_gpus=args.num_gpus, resources=args.custom_resources)
class Actor:
    pass


ray.init(namespace="default_namespace")
Actor.options(name=args.name, lifetime="detached").remote()

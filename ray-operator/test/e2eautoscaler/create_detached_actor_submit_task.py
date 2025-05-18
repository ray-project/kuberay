"""This script create an actor that will continuously submits tasks to simulate user workload"""

import ray
import sys
import argparse
import time

parser = argparse.ArgumentParser()
parser.add_argument("--num-cpus", type=float, default=1)
parser.add_argument("--num-gpus", type=float, default=0)
parser.add_argument("--num-custom-resources", type=float, default=0)
parser.add_argument("--tasks-per-batch", type=int, default=10)
parser.add_argument("--task-duration", type=float, default=1)
args = parser.parse_args()


@ray.remote
def task():
    time.sleep(args.task_duration)


@ray.remote(
    num_cpus=args.num_cpus,
    num_gpus=args.num_gpus,
    resources={"CustomResource": args.num_custom_resources},
)
class Actor:
    def submit_tasks(self, num_tasks):
        while True:
            futures = [task.remote() for _ in range(num_tasks)]
            ray.get(futures)  # wait for current batch to complete before next batch


ray.init(namespace="default_namespace")
actor = Actor.options(lifetime="detached").remote()
actor.submit_tasks.remote(args.tasks_per_batch)

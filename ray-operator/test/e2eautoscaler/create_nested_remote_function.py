import ray
import time
import argparse

parser = argparse.ArgumentParser()
parser.add_argument('--num-cpus', type=float, default=1)
args = parser.parse_args()

@ray.remote(num_cpus=args.num_cpus)
def child_task1():
    time.sleep(5)

@ray.remote(num_cpus=args.num_cpus)
def child_task2():
    time.sleep(30)

@ray.remote(num_cpus=args.num_cpus)
def parent_task():
    future_list = [child_task1.remote(), 
                        child_task2.remote()]

    # `child_task1` is intended to be scheduled on the same node as the parent task.
    # `child_task2` is intended to be scheduled on another node.
    # After `child_task1` is finished, there'll be no resource utilization on the node where the parent task is running.
    # However, the node where the parent task is running should not be downscaled until `child_task2` is finished
    ray.get(future_list)



ray.init(namespace="default_namespace")
ray.get(parent_task.remote())
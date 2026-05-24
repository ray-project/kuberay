import argparse
from datetime import datetime, timezone

import ray

parser = argparse.ArgumentParser()
parser.add_argument("name")
parser.add_argument("--num-cpus", type=float, default=1)
parser.add_argument("--num-gpus", type=float, default=0)
parser.add_argument("--num-custom-resources", type=float, default=0)
args = parser.parse_args()


def log(message: str):
    ts = datetime.now(timezone.utc).isoformat()
    print(f"[{ts}] create_detached_actor: {message}", flush=True)


@ray.remote(
    max_restarts=-1,
    num_cpus=args.num_cpus,
    num_gpus=args.num_gpus,
    resources={"CustomResource": args.num_custom_resources},
)
class Actor:
    pass


ray.init(namespace="default_namespace")
log(
    f"start name={args.name} num_cpus={args.num_cpus} "
    f"num_gpus={args.num_gpus} num_custom_resources={args.num_custom_resources}"
)
try:
    ray.get_actor(args.name, namespace="default_namespace")
    log("actor_exists_before_create=true")
except ValueError:
    log("actor_exists_before_create=false")

Actor.options(name=args.name, lifetime="detached").remote()
log("remote_create_submitted=true")

try:
    ray.get_actor(args.name, namespace="default_namespace")
    log("actor_exists_after_create=true")
except ValueError:
    log("actor_exists_after_create=false")

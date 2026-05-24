import ray
import sys
from datetime import datetime, timezone


def log(message: str):
    ts = datetime.now(timezone.utc).isoformat()
    print(f"[{ts}] terminate_detached_actor: {message}", flush=True)


ray.init(namespace="default_namespace")
actor_name = sys.argv[1]
log(f"start name={actor_name}")
try:
    detached_actor = ray.get_actor(actor_name, namespace="default_namespace")
    log("actor_exists_before_kill=true")
except ValueError:
    log("actor_exists_before_kill=false")
    raise

ray.kill(detached_actor)
log("kill_submitted=true")

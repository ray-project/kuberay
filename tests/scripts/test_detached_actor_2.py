import ray
import time
import sys

ray.init(address='ray://127.0.0.1:10001', namespace=sys.argv[1])

def retry_with_timeout(func, timeout=90):
    err = None
    for _ in range(timeout):
        try:
            return func()
        except BaseException as e:
            err = e
        finally:
            time.sleep(1)
    raise err

def get_detached_actor():
    return ray.get_actor("testCounter")

tc = retry_with_timeout(get_detached_actor)
val = retry_with_timeout(lambda: ray.get(tc.increment.remote()))
print(f"val: {val}")

# The actual value should be 1 rather than 2. Ray will launch all registered actors when
# the ray cluster restarts, but the internal state of the state will not be restored.
assert(val == 1)

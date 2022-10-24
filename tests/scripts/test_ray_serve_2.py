import ray
import time
import sys
from ray import serve

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

retry_with_timeout(lambda: ray.init(address='ray://127.0.0.1:10001', namespace=sys.argv[1]))
retry_with_timeout(lambda:serve.start(detached=True))
val = retry_with_timeout(lambda: ray.get(serve.get_deployment("MyModelDeployment").get_handle().remote()))
print(val)
assert(val == "Hello world!")


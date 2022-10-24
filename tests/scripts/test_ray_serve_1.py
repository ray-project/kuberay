import requests
from starlette.requests import Request
import ray
from ray import serve
import time
import sys

# 1: Define a Ray Serve model.
@serve.deployment(route_prefix="/")
class MyModelDeployment:
    def __init__(self, msg: str):
        self._msg = msg

    def __call__(self):
        return self._msg

ray.init(address='ray://127.0.0.1:10001', namespace=sys.argv[1])
# 2: Deploy the model.
handle = serve.run(MyModelDeployment.bind(msg="Hello world!"))
# 3: Query the deployment and print the result.
val = ray.get(handle.remote())
print(val)
assert(val == "Hello world!")

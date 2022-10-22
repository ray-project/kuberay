import ray
import sys
import ray.serve as serve
import os
import requests
from ray._private.test_utils import wait_for_condition

ray.init(address='ray://127.0.0.1:10001', namespace=sys.argv[1])
serve.start(detached=True)

@serve.deployment
def d(*args):
    return "HelloWorld"
d.deploy()
val = ray.get(d.get_handle().remote())
print(val)
assert(val == "HelloWorld")

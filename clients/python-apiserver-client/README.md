# Python client for KubeRay API server

This Python client is currently only supporting Ray cluster mangement through usage of the `API server` Ray API. It implements all of the current functionality of the API server (and the operator) and provide pythonic APIs to the capabilities.

The package supports well documented in the code Python objects that can be used to build and recieve payloads for creation, listing and delition of [template](https://ray-project.github.io/kuberay/components/apiserver/#compute-template) and Ray clusters.

The main class of the package is [KubeRayAPIs](python_client/kuberay_apis.py) that implements all of the functionality. It leverages [templates](python_client/params/templates.py) and [cluster](python_client/params/cluster.py) definitions, allowing to specify all required parameters as straight Python classes. Additional (intermediate) definitons are provided (see [environment variables](python_client/params/environmentvariables.py), [volumes](python_client/params/volumes.py), [head group](python_client/params/headnode.py) and [worker group](python_client/params/workernode.py))

## Prerequisites

It is also expected that the `kuberay operator` [Installation instructions are here.](https://github.com/ray-project/kuberay#quick-start) and `api server` [Installation instructions are here.](https://ray-project.github.io/kuberay/components/apiserver) are installed

## Testing

Test files [parameters_test](api_params_test.py) and [api_test](kuberay_api_test.py) exersize the packeage functionality and can also be used as a guide for API usage.

Note that [api_test](kuberay_api_test.py) requires creation of the additional [configmap](../../apiserver/test/job/code.yaml) in the default namespace

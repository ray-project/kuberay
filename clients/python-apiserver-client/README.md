# Python client for KubeRay API server

This Python client is currently only supporting Ray cluster management through usage of the `API server` Ray API. It implements all of the current functionality of the API server (and the operator) and provide pythonic APIs to the capabilities.

The package supports Python objects (documented in the code ) that can be used to build and receive payloads for creation, listing and deleting of [template](https://ray-project.github.io/kuberay/components/apiserver/#compute-template) and Ray clusters.

The main class of the package is [KubeRayAPIs](python_apiserver_client/kuberay_apis.py) that implements all of the functionality. It leverages [templates](python_apiserver_client/params/templates.py) and [cluster](python_apiserver_client/params/cluster.py) definitions, allowing to specify all required parameters as straight Python classes. Additional (intermediate) definitions are provided (see [environment variables](python_apiserver_client/params/environmentvariables.py), [volumes](python_apiserver_client/params/volumes.py), [head group](python_apiserver_client/params/headnode.py) and [worker group](python_apiserver_client/params/workernode.py))

## Prerequisites

It is also expected that the `kuberay operator` ([Installation instructions are here.](https://github.com/ray-project/kuberay#quick-start)) and `api server` ([Installation instructions are here.](https://ray-project.github.io/kuberay/components/apiserver)) are installed

## Development

Start by installing `setup-tools`

```shell
pip3 install -U setuptools
```

Now install our library locally. From the directory `path/to/kuberay/clients/python_apiserver_client` execute

```shell
pip3 install -e .
```

## Testing

Test files [parameters_test](python_apiserver_client_test/api_params_test.py) and [api_test](python_apiserver_client_test/kuberay_api_test.py) exercise the package functionality and can also be used as a guide for API usage.

Note that [api_test](python_apiserver_client_test/kuberay_api_test.py) requires installation of the package and creation of the additional [configmap](../../apiserver/test/job/code.yaml) in the default namespace

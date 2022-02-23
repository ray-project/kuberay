# Troubleshooting handbook

## Introduction

This page will give you some guild on troubleshooting for some situations when you deploy and use the kuberay.

## Ray Version Compatibility

### Problem

For every running ray cluster, when we try to connect with the client, we must be careful about the python and ray version we used. There are several  issues report failures related to version imcompatibility: [#148](https://github.com/ray-project/kuberay/issues/148), [#21549](https://github.com/ray-project/ray/issues/21549). Therefore there is a reminder for troubleshooting when come up with that situation.

### Error cases

In the ray client initialization, there are several checks will be executed in [ray-project/ray/util/client/\_\_init\_\_.py:L115-L137](https://github.com/ray-project/ray/blob/master/python/ray/util/client/__init__.py#L115-L137).

Common cases would be like:

```bash
...

RuntimeError: Python minor versions differ between client and server: client is 3.8.10, server is 3.7.7
```

or:

```bash
...
RuntimeError: Client Ray installation incompatible with server: client is 2021-05-20, server is 2021-12-07
```

Some cases may not be so clear:

```bash
ConnectionAbortedError: Initialization failure from server:
Traceback (most recent call last):
...
'AttributeError: ''JobConfig'' object has no attribute ''_parsed_runtime_env''
```

```
Traceback (most recent call last):
...
RuntimeError: Version mismatch: The cluster was started with:
    Ray: 1.9.0
    Python: 3.7.7
This process on node NODE_ADDRESS was started with:
    Ray: 1.10.0
    Python: 3.7.7
```

### Solution

In above cases, you will need to check if the client ray version is compatible with the images version in the ray cluster's configuration. 

For example, when you deployed `kuberay/ray-operator/config/samples/ray-cluster.mini.yaml`, you need to be aware that `spec.rayVersion` and images version is the same with your expect ray release and same with your ray client version.

---
**NOTE:**

_In ray code, the version check will only go through major and minor version, so the python and ray image's minor version match is enough. Also the ray upstream community provide different python version support from 3.6 to 3.9, you can choose the image to match your python version._

---
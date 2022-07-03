# Workspace Container Image

This folder contains Dockerfile and scripts to build workspace container image. 
The Dockerfiles should be parameterized and this allows us to use scripts to build all images once. 

## JupyterLab Extensions

JupyterLab is designed as an extensible environment. We bring lots of ML centric extensions to Jupyter Notebooks, including:

- In-built Ray Dashboard
- Ray Cluster, Jobs and Serve Deployments management
- Reusable Code Snippets
- Notebook versioning based on Git integration
- IDE development experiences like autocomplete, autoformat, go-to-definition, etc
- Rich GPU usage visualizations

## Supported Kernels

A `kernel` is a program that runs and introspects the user’s code. `IPython` includes a kernel for Python code. 
In our container images, we pre-configured a few common Python kernels to help user quickly start coding.

For Python, we will provide a few kernels for quick onboard.

- ray_ml_p37 (TensorFlow2.6(+Keras2), PyTorch 1.9 with Py3.7)
- ray_core_p37
- python3

## Development 

To make sure development process is exact same as image build system, we need to go to project root folder.

```
docker build -t kuberay/workspace:testing -f docker/workspace/Dockerfile .
```

We can start docker image locally and visit `http://localhost:8888` to start workspace.
```
docker run -it --rm -p 8888:8888 kuberay/workspace:testing
```

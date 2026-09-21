# RayJob

RayJob is a custom resource definition (CRD). **KubeRay operator** will listen to the resource events about RayJob and create related Kubernetes resources (e.g. Pod & Service). Hence, **KubeRay operator** installation and **CRD** registration are required for this guide.

## Prerequisites
See [kuberay-operator/README.md](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/README.md) for more details.
* Helm
* Install custom resource definition and KubeRay operator (covered by the following end-to-end example.)

## End-to-end example
Find full documentation and examples here: https://github.com/ray-project/kuberay/blob/master/docs/guidance/rayjob.md

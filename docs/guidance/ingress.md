## Ingress Usage

### Prerequisite

It's user's responsibility to install ingress controller by themselves. Technically, any ingress controller implementation should work well. 

In order to pass through the customized ingress configuration, you can annotate `RayCluster` object and controller will pass to ingress object.

`kubernetes.io/ingress.class` is recommended. 

> Note: If the ingressClassName is omitted, a default Ingress class should be defined. Please make sure default ingress class is created.

```
apiVersion: ray.io/v1alpha1
kind: RayCluster
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx -> this is required
spec:
  rayVersion: '1.9.2'
  headGroupSpec:
    serviceType: NodePort
    enableIngress: true -> enables ingress
```

# Ray Operator for Kubernetes

> Note: this project is still experimental

## Why was this operator created? 

This decision to create a new operator wasn't made lightly, but it was necessary due to some drawbacks 
that are present in upstream [ray-operator](https://docs.ray.io/en/releases-1.2.0/cluster/k8s-operator.html). 
Current upstream operator mix operator and autoscaler in a single component, which 
make us really hard to manage multiple versions of Ray cluster in multi-tenant environments. What's more, 
quality and performance of python based operator is still hard to match commonly used golang operator framework. 

Ideally, this operator will extract autoscaler as a separate component and create one for each cluster.
It is responsible for make scaling decisions and tell operator how many pods to bring up or which pods to scale in. The good thing is we are not alone, 
Anyscale, Ant Group and Microsoft kicked off this discussion on [designs](https://docs.google.com/document/d/1DPS-e34DkqQ4AeJpoBnSrUM8SnHnQVkiLlcmI4zWEWg/edit?ts=5f906e13#) in 2020.
We will follow the path of the predecessors and make it better. 

## Features

- Support multiple ray version and rayclusters crd version in multi-tenant environments.
- Flexible options to expose head service to fit different network environments. 
- Support ray cluster rolling upgrade. (coming soon)
- Expose rich telemetry of ray clusters. (coming soon)

## Quick Start

1. Build image and update
```
git clone https://github.com/ray-project/ray-contrib.git
cd ray-contrib/bytedance && IMG=${org}/${repo}:${tag} make docker-build
# push your images to registry
```

2. Deploy ray-operator

```
IMG=${org}/${repo}:${tag} make deploy
```

2. Deploy simple ray cluster

```
kubectl apply -f https://raw.githubusercontent.com/ray-project/ray-contrib/master/bytedance/examples/cluster-simple.yaml
```


## Discussion, Contribution and Support

If you have questions or what to get the latest project news, you can connect with us in the following ways:
 
- Chat with team members (@Jiaxin Shan, @Mengyuan Chao) in Ray Slack Group
- Create Github issues
 
See also our [contributor guide](./CONTRIBUTING.md) for more details on how to get involved.

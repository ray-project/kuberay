# Serve a StableDiffusion text-to-image model using RayService

> **Note:** The Python files for the Ray Serve application and its client are in the [ray-project/serve_config_examples](https://github.com/ray-project/serve_config_examples) repo 
and [the Ray documentation](https://docs.ray.io/en/latest/serve/tutorials/stable-diffusion.html).

## Step 1: Create a Kubernetes cluster with GPUs

You can follow [aws-eks-gpu-cluster.md](./aws-eks-gpu-cluster.md) to create an AWS EKS cluster with GPUs.
For this example, a working configuration for the CPU node group and GPU node group is:

* CPU node group
  * Instance type: [**m5.xlarge**](https://aws.amazon.com/ec2/instance-types/m5/) (4 vCPU; 16 GB RAM)
  * Disk size: 256 GB
  * Desired size: 1, Min size: 0, Max size: 1

* GPU node group
  * Add a Kubernetes taint to prevent CPU Pods from being scheduled on this GPU node group
    * Key: ray.io/node-type, Value: worker, Effect: NoSchedule
  * AMI type: Bottlerocket NVIDIA (BOTTLEROCKET_x86_64_NVIDIA)
  * Instance type: [**g5.12xlarge**](https://aws.amazon.com/ec2/instance-types/g5/) (4 GPU; 96 GB GPU Memory; 48 vCPUs; 192 GB RAM)
  * Disk size: 1024 GB
  * Desired size: 1, Min size: 0, Max size: 1

## Step 2: Install KubeRay operator

Follow [this document](../../helm-chart/kuberay-operator/README.md) to install the nightly KubeRay operator via 
Helm. Note that the YAML file in Step 3 uses `serveConfigV2`, which is first supported by KubeRay v0.6.0.

## Step 3: Install a RayService

```sh
# path: ray-operator/config/samples/
kubectl apply -f ray-service.stable-diffusion.yaml
```

* The `tolerations` for workers must match the taints on the GPU node group. Without the tolerations, worker Pods won't be scheduled on GPU nodes.
    ```yaml
    # Please add the following taints to the GPU node.
    tolerations:
        - key: "ray.io/node-type"
        operator: "Equal"
        value: "worker"
        effect: "NoSchedule"
    ```
* Install `diffusers` in `runtime_env` as it is not included by default in the `ray-ml` image.

## Step 4: Forward the port of Serve

```sh
kubectl port-forward svc/stable-diffusion-serve-svc 8000
```

Note that the Serve service will be created after the Serve applications are ready and running. This process may take approximately 1 minute after all Pods in the RayCluster are running.

## Step 5: Send a request to the text-to-image model

* Step 5.1 Update `prompt` in [stable_diffusion_req.py](https://github.com/ray-project/serve_config_examples/blob/master/stable_diffusion/stable_diffusion_req.py).
* Step 5.2: Send a request to the Stable Diffusion model.
  ```sh
  python stable_diffusion_req.py
  # Check output.png
  ```

![image](../images/stable_diffusion_example.png)
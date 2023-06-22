# Start Amazon EKS Cluster with GPUs for KubeRay

## Step 1: Create a Kubernetes cluster on Amazon EKS

Follow the first two steps in [this AWS documentation](https://docs.aws.amazon.com/eks/latest/userguide/getting-started-console.html#)
to: (1) Create your Amazon EKS cluster (2) Configure your computer to communicate with your cluster.

## Step 2: Create node groups for the Amazon EKS cluster

You can follow "Step 3: Create nodes" in [this AWS documentation](https://docs.aws.amazon.com/eks/latest/userguide/getting-started-console.html#) to create node groups. The following section provides more detailed information.

### Create a CPU node group

Typically, we do not run GPU workloads on the Ray head. Create a CPU node group for all Pods except Ray GPU 
workers, such as the KubeRay operator, Ray head, and CoreDNS Pods.

* A common configuration working for most KubeRay examples:
  * Instance type: [**m5.xlarge**](https://aws.amazon.com/ec2/instance-types/m5/) (4 vCPU; 16 GB RAM)
  * Disk size: 256 GB
  * Desired size: 1, Min size: 0, Max size: 1

### Create a GPU node group

Create a GPU node group for Ray GPU workers.

1. A common configuration working for most KubeRay examples:
   * AMI type: Bottlerocket NVIDIA (BOTTLEROCKET_x86_64_NVIDIA)
   * Instance type: [**g5.xlarge**](https://aws.amazon.com/ec2/instance-types/g5/) (1 GPU; 24 GB GPU Memory; 4 vCPUs; 16 GB RAM)
   * Disk size: 1024 GB
   * Desired size: 1, Min size: 0, Max size: 1

2. **Please follow Step 4 to install the NVIDIA device plugin.**
   * If you use `AMI type: Bottlerocket NVIDIA`, there is no need to install NVIDIA device plugin.
   * For other AMI types, you may need to install the NVIDIA device plugin DaemonSet in order to run GPU-enabled containers in your Amazon EKS cluster.
   If the GPU nodes have taints, you need to add `tolerations` to `nvidia-device-plugin.yml` to enable the DaemonSet to schedule Pods on the GPU nodes."

3. Add a Kubernetes taint to prevent scheduling CPU Pods on this GPU node group. For KubeRay examples, we add the following taint to the GPU nodes: `Key: ray.io/node-type, Value: worker, Effect: NoSchedule`, and include the corresponding `tolerations` for GPU Ray Pods.

> Warning: GPU nodes are extremely expensive. Please remember to delete the cluster if you no longer need it.

## Step 3: Verify the node groups

> **Note:** If you encounter permission issues with `eksctl`, you can navigate to your AWS account's webpage and copy the
credential environment variables, including `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN`,
from the "Command line or programmatic access" page.

```sh
eksctl get nodegroup --cluster ${YOUR_EKS_NAME}

# CLUSTER         NODEGROUP       STATUS  CREATED                 MIN SIZE        MAX SIZE        DESIRED CAPACITY        INSTANCE TYPE   IMAGE ID                        ASG NAME                           TYPE
# ${YOUR_EKS_NAME}     cpu-node-group  ACTIVE  2023-06-05T21:31:49Z    0               1               1                       m5.xlarge       AL2_x86_64                      eks-cpu-node-group-...     managed
# ${YOUR_EKS_NAME}     gpu-node-group  ACTIVE  2023-06-05T22:01:44Z    0               1               1                       g5.12xlarge     BOTTLEROCKET_x86_64_NVIDIA      eks-gpu-node-group-...     managed
```

## Step 4: Install the DaemonSet for NVIDIA device plugin

> **Note:** If you encounter permission issues with `kubectl`, you can follow "Step 2: Configure your computer to communicate with your cluster"
in the [AWS documentation](https://docs.aws.amazon.com/eks/latest/userguide/getting-started-console.html#).

Install the DaemonSet for NVIDIA device plugin to run GPU enabled containers in your Amazon EKS cluster. You can refer to the [Amazon EKS optimized accelerated Amazon Linux AMIs](https://docs.aws.amazon.com/eks/latest/userguide/eks-optimized-ami.html#gpu-ami)
or [NVIDIA/k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin) repository for more details.

```sh
# Install the DaemonSet
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.9.0/nvidia-device-plugin.yml

# Verify that your nodes have allocatable GPUs 
kubectl get nodes "-o=custom-columns=NAME:.metadata.name,GPU:.status.allocatable.nvidia\.com/gpu"

# Example output:
# NAME                                GPU
# ip-....us-west-2.compute.internal   4
# ip-....us-west-2.compute.internal   <none>
```
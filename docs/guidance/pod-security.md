# Pod Security

Kubernetes defines three different Pod Security Standards, including `privileged`, `baseline`, and `restricted`, to broadly cover the security spectrum. The `privileged` standard allows users to do known privilege escalations, and thus it is not safe enough for security-critical applications.

This document describes how to configure RayCluster YAML file to apply `restricted` Pod security standard. The following references can help you understand this document better:

* [Kubernetes - Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted)
* [Kubernetes - Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/)
* [Kubernetes - Auditing](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)
* [KinD - Auditing](https://kind.sigs.k8s.io/docs/user/auditing/)

# Step1: Create a KinD cluster
```bash
# Please use Kubernetes >= 1.23. I use 1.24 as my environment.
kind create cluster --config kind-config.yaml
```
The `kind-config.yaml` enables audit logging with the audit policy defined in `audit-policy.yaml`. The `audit-policy.yaml` defines an auditing policy to listen to the Pod events in the namespace `pod-security`. With this policy, we can check whether our Pods violate the policies in `restricted` standard or not.

# Step2: Check the audit logs
```bash
docker exec kind-control-plane cat /var/log/kubernetes/kube-apiserver-audit.log
```
The log should be empty because the namespace `pod-security` does not exist.

# Step3: Create the `pod-security` namespace
```bash
kubectl create ns pod-security
kubectl label --overwrite ns pod-security \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/warn-version=latest \
  pod-security.kubernetes.io/audit=restricted \
  pod-security.kubernetes.io/audit-version=latest \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/enforce-version=latest
```
With the `pod-security.kubernetes.io` labels, the built-in Kubernetes Pod security admission controller will apply the `restricted` Pod security standard to all Pods in the namespace `pod-security`. The label `pod-security.kubernetes.io/enforce=restricted` means that the Pod will be rejected if it violate the policies defined in `restricted` security standard. See [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) for more details about the labels.

# Step4: Install a KubeRay operator
```bash
pushd helm-chart/kuberay-operator
helm install kuberay-operator .
popd
```

# Step5: Create a RayCluster (Choose either Step5.1 or Step5.2)
* If you choose Step5.1, no Pod will be created in the namespace `pod-security`.
* If you choose Step5.2, Pods can be created successfully.

## Step5.1: Create a RayCluster without proper `securityContext` configurations
```bash
kubectl apply -n pod-security -f ray-operator/config/samples/ray-cluster.complete.yaml

# Wait 20 seconds and check audit logs for the error messages.
docker exec kind-control-plane cat /var/log/kubernetes/kube-apiserver-audit.log

# Example error messagess
# "pods \"raycluster-complete-head-fkbf5\" is forbidden: violates PodSecurity \"restricted:latest\": allowPrivilegeEscalation != false (container \"ray-head\" must set securityContext.allowPrivilegeEscalation=false) ...
```
No Pod will be created in the namespace `pod-security`, and check audit logs for error messages.

## Step5.2: Create a RayCluster with proper `securityContext` configurations
```bash
kubectl apply -n pod-security -f ray-operator/config/samples/ray-cluster.pod-security.yaml

# Wait for the RayCluster convergence and check audit logs for the messages.
docker exec kind-control-plane cat /var/log/kubernetes/kube-apiserver-audit.log
```
The RayCluster will work as expectation (1 head pod + 1 worker pod).

# Step6: Test the functionality with a simple job.
```bash
# Log in to the Pod
kubectl exec -it -n pod-security $HEAD_POD -- bash

# 
```


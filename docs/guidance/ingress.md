## Ingress Usage

### Prerequisite

It's user's responsibility to install ingress controller by themselves. Technically, any ingress controller implementation should work well. In order to pass through the customized ingress configuration, you can annotate `RayCluster` object and the controller will pass the annotations to the ingress object.

### Example: Nginx Ingress on KinD
```sh
# Step1: Create a KinD cluster with `extraPortMappings` and `node-labels`
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
EOF

# Step2: Install NGINX ingress
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s

# Step3: Install KubeRay operator
pushd helm-chart/kuberay-operator
helm install kuberay-operator .

# Step4: Install RayCluster with Nginx ingress. See https://github.com/ray-project/kuberay/pull/646
#        for the explanations of `ray-cluster.ingress.yaml`. Some fields are worth to discuss further:
#
#        (1) metadata.annotations.kubernetes.io/ingress.class: nginx => required
#        (2) spec.headGroupSpec.enableIngress: true => required
#        (3) metadata.annotations.nginx.ingress.kubernetes.io/rewrite-target: /$1 => required for nginx.
popd
kubectl apply -f ray-operator/config/samples/ray-cluster.ingress.yaml

# Step5: Check ingress created by Step4.
kubectl describe ingress raycluster-ingress-head-ingress

# [Example]
# ...
# Rules:
# Host        Path  Backends
# ----        ----  --------
# *
#             /raycluster-ingress/(.*)   raycluster-ingress-head-svc:8265 (10.244.0.11:8265)
# Annotations:  nginx.ingress.kubernetes.io/rewrite-target: /$1

# Step6: Check `<ip>/raycluster-ingress/` on your browser. You will see the Ray Dashboard.
#        [Note] The forward slash at the end of the address is necessary. `<ip>/raycluster-ingress`
#               will report "404 Not Found".
```
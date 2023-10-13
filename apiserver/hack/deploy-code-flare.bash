#!/bin/bash
set -euo pipefail
ACTION=$1
CODEFLARE_OPERATOR_VERSION=$2
[[ -z ${ACTION} ]] && echo "An action parameter is required" && exit 1
[[ -z ${CODEFLARE_OPERATOR_VERSION} ]] && echo "A code flare operator version parameter is required" && exit 1


TMPDIR=$(mktemp -d)

cat <<EOF >> ${TMPDIR}/kustomization.yaml
resources: 
- https://github.com/project-codeflare/codeflare-operator.git/config/default/?ref=${CODEFLARE_OPERATOR_VERSION}
kind: Kustomization
patches:
- patch: |-
    - op: replace
      path: /spec/template/spec/containers/0/imagePullPolicy
      value: IfNotPresent
  target:
    kind: Deployment
    name: codeflare-operator-manager
    namespace: openshift-operators
    version: v1
images:
- name: controller
  newName: quay.io/project-codeflare/codeflare-operator
  newTag: ${CODEFLARE_OPERATOR_VERSION}
EOF
cd ${TMPDIR} && kustomize build | kubectl ${ACTION} -f -

cat <<EOF | kubectl ${ACTION} -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mcad-controller-rayclusters
rules:
  - apiGroups:
      - ray.io
    resources:
      - rayclusters
      - rayclusters/finalizers
      - rayclusters/status
      - rayjobs
      - rayjobs/finalizers
      - rayjobs/status
    verbs:
      - get
      - list
      - watch
      - create
      - update
      - patch
      - delete
EOF

cat <<EOF | kubectl ${ACTION} -f -
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: mcad-controller-rayclusters
subjects:
  - kind: ServiceAccount
    name: codeflare-operator-controller-manager
    namespace: openshift-operators
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: mcad-controller-rayclusters
EOF

{{- define "{{ .Chart.Name }}.head.env" }}
- name: CPU_REQUEST
  value: {{ .Values.ray.head.num_cpus }}
- name: CPU_LIMITS
  value: {{ .Values.ray.head.num_cpus }}
- name: MEMORY_LIMITS
  valueFrom:
    resourceFieldRef:
      containerName: ray-head
      resource: limits.memory
- name: MEMORY_REQUESTS
  valueFrom:
    resourceFieldRef:
      containerName: ray-head
      resource: requests.memory
- name: MY_POD_IP
  valueFrom:
    fieldRef:
      fieldPath: status.podIP
- name: TYPE
  value: head
- name: AUTOSCALER_HEARTBEAT_TIMEOUT_S
  value: "240"
{{ if .Values.ray.head.containerEnv }}
{{- toYaml .Values.ray.head.containerEnv }}
{{ end }}

{{- if .Values.ray.head.envFrom -}}
{{- toYaml .Values.ray.head.envFrom -}}
{{ end }}
{{- end -}}

{{- define "{{ .Chart.Name }}.head.autoscaler" -}}
# The Ray autoscaler sidecar to the head pod
- name: autoscaler
  # TODO: Use released Ray version starting with Ray 1.12.0.
  image: {{ .Values.ray.autoscaler.image }}
  imagePullPolicy: IfNotPresent
  env:
    - name: RAY_CLUSTER_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    - name: RAY_CLUSTER_NAME
      # This value must match the metadata.name of the RayCluster CR.
      # The autoscaler uses this env variable to determine which Ray CR to interact with.
      # TODO: Match with CR name automatically via operator, Helm, and/or Kustomize.
      value: {{ include "ray-cluster.fullname" . }}
  command: ["ray"]
  args:
    - "kuberay-autoscaler"
    - "--cluster-name"
    - "$(RAY_CLUSTER_NAME)"
    - "--cluster-namespace"
    - "$(RAY_CLUSTER_NAMESPACE)"
  resources: {{- toYaml .Values.ray.autoscaler.resources | nindent 4 }}
  volumeMounts: {{- toYaml .Values.ray.autoscaler.volumeMounts | nindent 4 }}
{{- end }}

{{- define "{{ .Chart.Name }}.worker.env" }}
- name:  RAY_DISABLE_DOCKER_CPU_WARNING
  value: "1"
- name: TYPE
  value: "worker"
- name: CPU_REQUEST
  valueFrom:
    resourceFieldRef:
      containerName: {{ $.containerName }}
      resource: requests.cpu
- name: CPU_LIMITS
  valueFrom:
    resourceFieldRef:
      containerName: {{ $.containerName }}
      resource: limits.cpu
- name: MEMORY_LIMITS
  valueFrom:
    resourceFieldRef:
      containerName: {{ $.containerName }}
      resource: limits.memory
- name: MEMORY_REQUESTS
  valueFrom:
    resourceFieldRef:
      containerName: {{ $.containerName }}
      resource: requests.memory
- name: MY_POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: MY_POD_IP
  valueFrom:
    fieldRef:
      fieldPath: status.podIP
- name: TYPE
  value: worker
{{ if $.containerEnv }}
{{- toYaml $.containerEnv }}
{{ end }}

{{- if $.envFrom -}}
{{- toYaml $.envFrom }}
{{ end }}
{{- end -}}

{{- define "{{ .Chart.Name }}.lifecycle" }}
  lifecycle:
    preStop:
      exec:
        command: ["/bin/sh","-c","ray stop"]
{{- end }}

{{- define "{{ .Chart.Name }}.worker.initContainers" }}
# the env var $RAY_IP is set by the operator if missing, with the value of the head service name
- name: init-myservice
  image: busybox:1.28
  command: ['sh', '-c', "until nslookup $RAY_IP.$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace).svc.cluster.local; do echo waiting for myservice; sleep 2; done"]
  resources:
    requests:
      cpu: 100m
      memory: 500Mi
    limits:
      cpu: 100m
      memory: 500Mi
{{- end }}

{{- define "{{ .Chart.Name }}.image" }}
image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
imagePullPolicy: {{ .Values.image.pullPolicy }}
{{- end }}

{{- define "{{ .Chart.Name }}.worker.group" }}
{{- $includeValues := . }}
{{- $ogValues := .Values }}
{{- range $key, $val := .Values.ray.workers }}
- replicas: {{ $val.replicaCount }}
  minReplicas: {{ $val.minReplicas }}
  maxReplicas: {{ $val.maxReplicas }}
  # logical group name
  groupName: "{{ $key }}"
  # if worker pods need to be added, we can simply increment the replicas
  # if worker pods need to be removed, we decrement the replicas, and populate the podsToDelete list
  # the operator will remove pods from the list until the number of replicas is satisfied
  # when a pod is confirmed to be deleted, its name will be removed from the list below
  #scaleStrategy:
  #  workersToDelete:
  #  - raycluster-complete-worker-small-group-bdtwh
  #  - raycluster-complete-worker-small-group-hv457
  #  - raycluster-complete-worker-small-group-k8tj7
  # the following params are used to complete the ray start: ray start --block --node-ip-address= ...
  rayStartParams:
    {{- range $initKey, $initVal := $val.initArgs }}
      {{ $initKey }}: {{ $initVal | quote }}
    {{- end }}
  #pod template
  template:
    metadata:
      {{- if $val.annotations }}
      # annotations for pod
      # the env var $RAY_IP is set by the operator if missing, with the value of the head service name
      annotations: {{- toYaml $val.annotations | nindent 10}}
      {{- end }}
      labels:
        # custom labels. NOTE: do not define custom labels start with `raycluster.`, they may be used in controller.
        # Refer to https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
        rayCluster: {{ $includeValues.Release.Name }} # will be injected if missing
        rayNodeType: worker # will be injected if missing, must be head or worker
        groupName: {{ $key }} # will be injected if missing
        {{- include "ray-cluster.labels" $includeValues | nindent 8 }}
        {{- if $val.labels }}
        {{- toYaml $val.labels | nindent 8 }}
        {{- end }}
    spec:
      {{- if $ogValues.imagePullSecrets }}
      imagePullSecrets: {{- toYaml $ogValues.imagePullSecrets | nindent 10 }}
      {{- end }}
      {{- if $val.nodeSelector }}
      nodeSelector: {{- toYaml $val.nodeSelector | nindent 8 }}
      {{- end }}
      initContainers:
      {{- include "{{ .Chart.Name }}.worker.initContainers" . | indent 8 }}
      containers:
        - name: {{ $val.containerName }}
          {{- include "{{ .Chart.Name }}.image" $includeValues | indent 10 }}
          # environment variables to set in the container.Optional.
          # Refer to https://kubernetes.io/docs/tasks/inject-data-application/define-environment-variable-container/
          env: {{- include "{{ .Chart.Name }}.worker.env" $val | indent 12 -}}
          {{- include "{{ .Chart.Name }}.lifecycle" . | indent 8 }}
          resources: {{ toYaml $val.resources | nindent 12}}
          ports: {{- toYaml $val.ports | nindent 12 }}
          {{- if $val.volumeMounts }}
          # use volumeMounts.Optional.
          # Refer to https://kubernetes.io/docs/concepts/storage/volumes/
          volumeMounts: {{- toYaml $val.volumeMounts | nindent 12 }}
          {{- end -}}
      {{- if $val.volumes }}
      # use volumes
      # Refer to https://kubernetes.io/docs/concepts/storage/volumes/
      volumes: {{- toYaml $val.volumes | nindent 10 }}
      {{- end -}}
      {{- if $val.tolerations }}
      tolerations: {{- toYaml $val.tolerations | nindent 10 }}
      {{- end -}}
      {{- if $val.affinity }}
      affinity: {{- toYaml $val.affinity | nindent 10 }}
      {{- end }}
{{ end }}
{{- end }}

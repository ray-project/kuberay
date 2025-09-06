export interface RayClusterListResponse {
  apiVersion: string;
  items: RayClusterItem[];
  kind: string;
  metadata: {
    continue: string;
    resourceVersion: string;
  };
}

export interface RayClusterItem {
  apiVersion: string;
  kind: string;
  metadata: {
    creationTimestamp: string;
    generation: number;
    name: string;
    namespace: string;
    resourceVersion: string;
    uid: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
    managedFields?: any[];
  };
  spec: {
    headGroupSpec: {
      rayStartParams: Record<string, string>;
      template: {
        spec: {
          containers: Container[];
        };
      };
    };
    rayVersion: string;
    workerGroupSpecs: WorkerGroupSpec[];
  };
  status: {
    availableWorkerReplicas: number;
    conditions: RayClusterCondition[];
    desiredCPU: string;
    desiredGPU: string;
    desiredMemory: string;
    desiredTPU: string;
    desiredWorkerReplicas: number;
    endpoints: {
      client: string;
      dashboard: string;
      "gcs-server": string;
      metrics: string;
    };
    head: {
      podIP: string;
      podName: string;
      serviceIP: string;
      serviceName: string;
    };
    lastUpdateTime: string;
    maxWorkerReplicas: number;
    minWorkerReplicas: number;
    observedGeneration: number;
  };
}

interface Container {
  image: string;
  name: string;
  ports?: ContainerPort[];
  resources?: {
    limits: Record<string, string | number>;
    requests: Record<string, string | number>;
  };
}

interface ContainerPort {
  containerPort: number;
  name: string;
  protocol: string;
}

interface WorkerGroupSpec {
  groupName: string;
  maxReplicas: number;
  minReplicas: number;
  numOfHosts: number;
  rayStartParams: Record<string, string>;
  replicas: number;
  template: {
    spec: {
      containers: Container[];
    };
  };
}

export interface RayClusterCondition {
  lastTransitionTime: string;
  message: string;
  reason: string;
  status: "True" | "False" | "Unknown";
  type:
    | "HeadPodReady"
    | "RayClusterProvisioned"
    | "RayClusterSuspended"
    | "RayClusterSuspending";
}

export interface ClusterRow {
  name: string;
  namespace: string;
  rayVersion: string;
  clusterState: ClusterStatus;
  createdAt: string;
  lastUpdateTime: string;
  availableWorkerReplicas: number;
  desiredWorkerReplicas: number;
  desiredCPU: string;
  desiredMemory: string;
  desiredGPU: string;
  headPodName: string;
  headServiceIP: string;
  endpoints: {
    client: string;
    dashboard: string;
    gcsServer: string;
    metrics: string;
  };
  links: {
    rayHeadDashboardLink: string;
  };
  conditions: RayClusterCondition[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export type ClusterStatus =
  | "READY"
  | "PENDING"
  | "FAILED"
  | "SUSPENDED"
  | "SUSPENDING"
  | "UNKNOWN";

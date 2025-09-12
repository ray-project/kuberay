export interface Container {
  image: string;
  name: string;
  ports?: ContainerPort[];
  resources?: {
    limits: Record<string, string | number>;
    requests: Record<string, string | number>;
  };
  env?: Array<{
    name: string;
    value: string;
  }>;
}

interface ContainerPort {
  containerPort: number;
  name: string;
  protocol: string;
}

export interface RayClusterStatus {
  desiredCPU: string;
  desiredGPU: string;
  desiredMemory: string;
  desiredTPU: string;
  head: Record<string, any>;
}

export interface RayClusterSpec {
  headGroupSpec: {
    rayStartParams: Record<string, string>;
    template: {
      metadata?: Record<string, any>;
      spec: {
        containers: Container[];
      };
    };
  };
  rayVersion: string;
  workerGroupSpecs?: WorkerGroupSpec[];
}

export interface WorkerGroupSpec {
  groupName: string;
  maxReplicas: number;
  minReplicas: number;
  numOfHosts: number;
  rayStartParams: Record<string, string>;
  replicas: number;
  scaleStrategy?: Record<string, any>;
  template: {
    metadata?: Record<string, any>;
    spec: {
      containers: Container[];
    };
  };
}

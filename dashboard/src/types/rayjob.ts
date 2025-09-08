import { Container, RayClusterStatus } from "./common";

export interface RayJobListResponse {
  apiVersion: string;
  items: RayJobItem[];
  kind: string;
  metadata: {
    continue: string;
    resourceVersion: string;
  };
}

export interface RayJobItem {
  apiVersion: string;
  kind: string;
  metadata: {
    creationTimestamp: string;
    finalizers?: string[];
    generation: number;
    managedFields?: any[];
    name: string;
    namespace: string;
    resourceVersion: string;
    uid: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec: {
    backoffLimit: number;
    rayClusterSpec: {
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
      workerGroupSpecs?: RayJobWorkerGroupSpec[];
    };
    submissionMode: string;
    ttlSecondsAfterFinished: number;
    entrypoint?: string;
    runtimeEnvYAML?: string;
    shutdownAfterJobFinishes?: boolean;
  };
  status: {
    dashboardURL: string;
    failed: number;
    jobDeploymentStatus: JobDeploymentStatus;
    rayClusterName: string;
    rayClusterStatus: RayClusterStatus;
    rayJobInfo: Record<string, any>;
    startTime: string;
    succeeded: number;
    jobId?: string;
    jobStatus?: JobStatus;
    message?: string;
    endTime?: string;
  };
}

interface RayJobWorkerGroupSpec {
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

export interface JobRow {
  name: string;
  namespace: string;
  jobStatus: {
    jobStatus: JobStatus;
    jobDeploymentStatus: JobDeploymentStatus;
  };
  createdAt: string;
  message: string;
  links: {
    rayHeadDashboardLink: string;
  };
  rayClusterName: string;
  submissionMode: string;
  rayVersion: string;
  clusterSpec: {
    headGroupSpec: {
      rayStartParams: Record<string, string>;
      template: {
        metadata?: {
          labels?: Record<string, string>;
        };
        spec: {
          containers: Container[];
        };
      };
    };
    rayVersion: string;
    workerGroupSpecs?: RayJobWorkerGroupSpec[];
  };
}

export type JobStatus =
  | "SUCCEEDED"
  | "FAILED"
  | "RUNNING"
  | "PENDING"
  | "STOPPED"
  | "WAITING"
  | "UNKNOWN";

export type JobDeploymentStatus =
  | "PENDING"
  | "RUNNING"
  | "SUCCEEDED"
  | "FAILED"
  | "STOPPED"
  | "UNKNOWN";

export type JobType = "All" | "Batch API" | "Ray";

import { ClusterRow, ClusterStatus } from "@/types/raycluster";
import { Job, JobRow, Jobs, Status } from "@/types/rayjob";

export const filterJobs = (
  jobs: Jobs,
  search: string,
  statusFilter: Status | null,
  typeFilter: number,
): JobRow[] =>
  jobs
    .map((job) => {
      if (!job.jobStatus) {
        job.jobStatus = "PENDING";
      }
      return job;
    })
    .filter((job: Job) => {
      // case-insensitive search
      if (
        statusFilter &&
        job.jobStatus.toUpperCase() !== statusFilter.toUpperCase()
      ) {
        return false;
      }
      if (search && !job.name.toUpperCase().includes(search.toUpperCase())) {
        return false;
      }
      if (
        typeFilter == 1 &&
        job.clusterSpec.headGroupSpec.labels["mlp.rbx.com/component"] !==
          "rayllmbatchinference"
      ) {
        return false;
      }
      return true;
    })
    .map(transformJob);

const transformJob = (job: Job): JobRow => {
  return {
    name: job.name,
    jobStatus: {
      jobStatus: job.jobStatus,
      jobDeploymentStatus: job.jobDeploymentStatus || "",
    },
    createdAt: job.createdAt,
    links: {
      rayGrafanaDashboardLink: job.rayGrafanaDashboardLink,
      logsLink: job.logsLink,
      rayHeadDashboardLink: job.rayHeadDashboardLink,
    },
    message: job.message || "",
  };
};

export const filterCluster = (
  clusters: ClusterRow[],
  search: string,
  statusFilter: ClusterStatus | null,
  typeFilter: number,
): ClusterRow[] => {
  return clusters.filter((cluster) => {
    if (statusFilter && cluster.clusterState !== statusFilter) {
      return false;
    }

    const searchUpper = search.toUpperCase();
    const nameMatch = cluster.name.toUpperCase().includes(searchUpper);
    const namespaceMatch = cluster.namespace
      .toUpperCase()
      .includes(searchUpper);

    if (!nameMatch && !namespaceMatch) {
      return false;
    }
    const isRayJob = clusterIsRayJob(cluster);

    if (typeFilter === 1 && isRayJob) {
      return false;
    }
    if (typeFilter === 2 && !isRayJob) {
      return false;
    }

    return true;
  });
};

export const clusterIsRayJob = (cluster: ClusterRow): boolean => {
  if (!cluster.labels) {
    return false;
  }

  const jobType = cluster.labels["mlp.rbx.com/component"];
  return jobType === "rayjob" || jobType === "rayllmbatchinference";
};

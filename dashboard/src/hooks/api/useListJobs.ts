import { ALL_NAMESPACES } from "@/utils/constants";
import { useNamespace } from "@/components/NamespaceProvider";
import fetcher from "@/utils/fetch";
import useSWR from "swr";
import { RayJobListResponse, RayJobItem, JobRow } from "@/types/rayjob";

export const useListJobs = (refreshInterval: number = 5000) => {
  const namespace = useNamespace();
  // We could use "isValidating" to indicate to the user that we are refreshing,
  // however, when isValidating is used, the component gets re-rendered even if
  // the data doesn't change, which we don't want.
  const { data, error, isLoading, mutate } = useSWR<RayJobListResponse>(
    // if no namespace is loaded yet, skip fetching.
    // Also, you can't reach all_namespaces in the kubeflow UI, but you can do it
    // when viewing the standalone app.
    namespace
      ? `${namespace == ALL_NAMESPACES ? `` : `/namespaces/${namespace}`}/rayjobs`
      : null,
    fetcher,
    {
      refreshInterval,
    },
  );

  const jobs: JobRow[] = data?.items
    ? data.items.map(convertRayJobItemToJobRow)
    : [];

  return {
    jobs,
    isLoading,
    error,
    mutate,
  };
};

const convertRayJobItemToJobRow = (item: RayJobItem): JobRow => {
  // Generate dashboard and logs links based on your URL pattern
  const generateDashboardLink = (dashboardURL: string) => {
    // Modify this based on your actual dashboard URL pattern
    return `http://${dashboardURL}`;
  };
  return {
    name: item.metadata.name,
    namespace: item.metadata.namespace,
    jobStatus: {
      jobStatus: item.status.jobStatus || "UNKNOWN",
      jobDeploymentStatus: item.status.jobDeploymentStatus,
    },
    createdAt: item.metadata.creationTimestamp,
    message: item.status.message || "",
    links: {
      rayHeadDashboardLink: generateDashboardLink(item.status.dashboardURL),
    },
    rayClusterName: item.status.rayClusterName,
    submissionMode: item.spec.submissionMode,
    rayVersion: item.spec.rayClusterSpec.rayVersion,
    clusterSpec: {
      headGroupSpec: item.spec.rayClusterSpec.headGroupSpec,
      rayVersion: item.spec.rayClusterSpec.rayVersion,
      workerGroupSpecs: item.spec.rayClusterSpec.workerGroupSpecs,
    },
  };
};

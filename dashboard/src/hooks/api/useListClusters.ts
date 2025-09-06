import { ALL_NAMESPACES } from "@/utils/constants";
import { useNamespace } from "@/components/NamespaceProvider";
import fetcher from "@/utils/fetch";
import useSWR from "swr";
import {
  ClusterRow,
  ClusterStatus,
  RayClusterCondition,
  RayClusterListResponse,
} from "@/types/raycluster";

export const useListClusters = (refreshInterval: number = 5000) => {
  const namespace = useNamespace();
  // We could use "isValidating" to indicate to the user that we are refreshing,
  // however, when isValidating is used, the component gets re-rendered even if
  // the data doesn't change, which we don't want.
  const { data, error, isLoading } = useSWR(
    // if no namespace is loaded yet, skip fetching.
    // Also, you can't reach all_namespaces in the kubeflow UI, but you can do it
    // when viewing the standalone app.
    namespace
      ? `${
          namespace == ALL_NAMESPACES ? `` : `/namespaces/${namespace}`
        }/rayclusters`
      : null,
    fetcher,
    {
      refreshInterval,
    },
  );

  return {
    clusters: data
      ? transformRayClusterResponse(data as RayClusterListResponse)
      : [],
    // jobs: jobs,
    // if no namespace is loaded yet, show loading indicator
    isLoading,
    error,
  };
};
const parseClusterStatus = (
  conditions: RayClusterCondition[],
): ClusterStatus => {
  const headPodReady = conditions.find((c) => c.type === "HeadPodReady");
  const provisioned = conditions.find(
    (c) => c.type === "RayClusterProvisioned",
  );
  const suspended = conditions.find((c) => c.type === "RayClusterSuspended");
  const suspending = conditions.find((c) => c.type === "RayClusterSuspending");

  if (suspended?.status === "True") {
    return "SUSPENDED";
  }
  if (suspending?.status === "True") {
    return "SUSPENDING";
  }

  if (headPodReady?.status === "True" && provisioned?.status !== "False") {
    return "READY";
  }

  if (headPodReady?.status === "False" || provisioned?.status === "False") {
    return "FAILED";
  }

  return "PENDING";
};

export const transformRayClusterResponse = (
  response: RayClusterListResponse,
): ClusterRow[] => {
  return response.items.map((item) => {
    const clusterState = parseClusterStatus(item.status.conditions);

    const generateLinks = () => {
      const serviceIP = item.status.head.serviceIP;
      const dashboardPort = item.status.endpoints.dashboard;

      return {
        rayHeadDashboardLink: `http://${serviceIP}:${dashboardPort}`,
      };
    };

    return {
      name: item.metadata.name,
      namespace: item.metadata.namespace,
      rayVersion: item.spec.rayVersion,
      clusterState,
      createdAt: item.metadata.creationTimestamp,
      lastUpdateTime: item.status.lastUpdateTime,
      availableWorkerReplicas: item.status.availableWorkerReplicas,
      desiredWorkerReplicas: item.status.desiredWorkerReplicas,
      desiredCPU: item.status.desiredCPU,
      desiredMemory: item.status.desiredMemory,
      desiredGPU: item.status.desiredGPU,
      headPodName: item.status.head.podName,
      headServiceIP: item.status.head.serviceIP,
      endpoints: {
        client: item.status.endpoints.client,
        dashboard: item.status.endpoints.dashboard,
        gcsServer: item.status.endpoints["gcs-server"],
        metrics: item.status.endpoints.metrics,
      },
      links: generateLinks(),
      conditions: item.status.conditions,
      labels: item.metadata.labels,
      annotations: item.metadata.annotations,
    };
  });
};

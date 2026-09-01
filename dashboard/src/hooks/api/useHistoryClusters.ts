import useSWR, { mutate } from "swr";
import { historyServerFetcher } from "@/utils/fetch";
import type { HistoryClusterInfoList } from "@/types/historyserver";
import { config } from "@/utils/constants";

// The history server's /enter_cluster route takes a {kind} segment
// (raycluster, rayjob, or rayservice; see historyserver/pkg/historyserver/router.go).
// Every entry returned by GET /clusters represents a RayCluster resource -
// OwnerKind/OwnerName on that response describe the RayJob/RayService that
// *owns* the cluster, not the kind of the entry itself - so this is always
// "raycluster" here.
const HISTORY_CLUSTER_KIND = "raycluster";

const isClusterScopedHistoryKey = (key: unknown) =>
  typeof key === "string" &&
  (key === "/api/v0/tasks" ||
    key === "/nodes?view=summary" ||
    key.startsWith("/api/v0/logs?") ||
    key.startsWith("log-content:"));

export const useHistoryClusters = (refreshInterval: number = 5000) => {
  const { data, error, isLoading } = useSWR<HistoryClusterInfoList>(
    "/clusters",
    historyServerFetcher,
    { refreshInterval },
  );

  const enterCluster = async (
    namespace: string,
    cluster: string,
    sessionName: string,
  ) => {
    const proxyEndpoint = (await config.getHistoryServerUrl()).proxyEndpoint;
    const res = await fetch(
      `${proxyEndpoint}/enter_cluster/${encodeURIComponent(namespace)}/${HISTORY_CLUSTER_KIND}/${encodeURIComponent(cluster)}/${encodeURIComponent(sessionName)}`,
      { method: "GET", credentials: "include" },
    );

    if (!res.ok) {
      throw new Error(
        `Failed to enter cluster: ${res.status} ${res.statusText}`,
      );
    }
    await mutate(isClusterScopedHistoryKey, undefined, { revalidate: false });
  };

  return {
    clusters: (data || []) as HistoryClusterInfoList,
    isLoading,
    error,
    enterCluster,
  };
};

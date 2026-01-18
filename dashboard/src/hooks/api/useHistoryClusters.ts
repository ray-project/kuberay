import useSWR, { mutate } from "swr";
import { historyServerFetcher } from "@/utils/fetch";
import type { HistoryClusterInfoList } from "@/types/historyserver";
import { config } from "@/utils/constants";

export const useHistoryClusters = (refreshInterval: number = 5000) => {
  const { data, error, isLoading } = useSWR<HistoryClusterInfoList | any>(
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
      `${proxyEndpoint}/enter_cluster/${encodeURIComponent(namespace)}/${encodeURIComponent(cluster)}/${encodeURIComponent(sessionName)}`,
      { method: "GET", credentials: "include" },
    );

    if (!res.ok) {
      throw new Error(
        `Failed to enter cluster: ${res.status} ${res.statusText}`,
      );
    }
    // Clean and refresh tasks cache
    mutate("/api/v0/tasks", undefined, { revalidate: false });
  };

  return {
    clusters: (data || []) as HistoryClusterInfoList,
    isLoading,
    error,
    enterCluster,
  };
};

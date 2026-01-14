import useSWR from "swr";
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
    await fetch(
      `${proxyEndpoint}/enter_cluster/${encodeURIComponent(namespace)}/${encodeURIComponent(cluster)}/${encodeURIComponent(sessionName)}`,
      { method: "GET", credentials: "include" },
    );
  };

  return {
    clusters: (data || []) as HistoryClusterInfoList,
    isLoading,
    error,
    enterCluster,
  };
};

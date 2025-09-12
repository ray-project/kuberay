import _ from "lodash";
import { useState } from "react";
import useSWR from "swr";
import { API_REFRESH_INTERVAL_MS } from "../../../common/constants";
import { getClusterList } from "../../../service/clusters";
import { useSorter } from "../../../util/hook";

export const useClusterList = () => {
//  const [isRefreshing, setRefresh] = useState(true);

  const { data } = useSWR(
    "useClusterList",
    async () => {
      const { data } = await getClusterList();
      return data;
    },
 //   { refreshInterval: isRefreshing ? API_REFRESH_INTERVAL_MS : 0 },
  );

  return {
    clustersList: data ?? [],
  };
};

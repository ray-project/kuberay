import { ClusterListRsp, ClusterDetail} from "../type/clusters";
import { get } from "./requestHandlers";

export const getClusterList = async () => {
  return await get<ClusterListRsp>("clusters");
};

export const getClusterDetail = async (id: string, ns: string, session: string) => {
  return await get<ClusterDetail>(`clusters/${ns}/${id}/${session}`);
};

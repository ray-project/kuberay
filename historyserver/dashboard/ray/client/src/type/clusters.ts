import { Actor } from "./actor";
import { Raylet } from "./raylet";
import { Worker } from "./worker";

export type ClusterDetail = {
  name: string;
  createTime: string;
  sessionName: string;
};

export type ClusterListRsp = ClusterDetail[]


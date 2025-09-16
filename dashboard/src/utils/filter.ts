import * as v2 from "./v2/filter";

function exportFuncByVersion() {
  // TODO: adapt to different versions of api
  return {
    filterJobs: v2.filterJobs,
    filterCluster: v2.filterCluster,
    clusterIsRayJob: v2.clusterIsRayJob,
    clusterIsNotebook: undefined,
  };
}

export const { filterJobs, filterCluster, clusterIsRayJob, clusterIsNotebook } =
  exportFuncByVersion();

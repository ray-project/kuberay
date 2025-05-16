import { ALL_NAMESPACES } from "@/utils/constants";
import { useNamespace } from "@/components/NamespaceProvider";
import { jobs } from "@/utils/exampleJobs";
import fetcher from "@/utils/fetch";
import useSWR from "swr";

export const useListJobs = (refreshInterval: number = 5000) => {
  const namespace = useNamespace();
  // We could use "isValidating" to indicate to the user that we are refreshing,
  // however, when isValidating is used, the component gets re-rendered even if
  // the data doesn't change, which we don't want.
  const { data, error, isLoading, mutate } = useSWR(
    // if no namespace is loaded yet, skip fetching.
    // Also, you can't reach all_namespaces in the kubeflow UI, but you can do it
    // when viewing the standalone app.
    namespace
      ? `${namespace == ALL_NAMESPACES ? `` : `/namespaces/${namespace}`}/jobs`
      : null,
    fetcher,
    {
      refreshInterval,
    }
  );

  return {
    jobs: data?.jobs ? data.jobs : [],
    // jobs: jobs,
    // if no namespace is loaded yet, show loading indicator
    isLoading,
    error,
  };
};

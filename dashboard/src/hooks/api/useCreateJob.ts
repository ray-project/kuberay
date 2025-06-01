import { mutate } from "swr";
import React from "react";
import { useSnackBar } from "@/components/SnackBarProvider";
import { useNamespace } from "@/components/NamespaceProvider";
import { useRouter } from "next/navigation";
import { config } from "@/utils/constants";
import { Job } from "@/types/rayjob";

// TODO: still hard-coded
async function _createJob(
  namespace: string,
  jobName: string,
  dockerImage: string,
  entrypoint: string
) {
  // curl -X 'POST' \
  //     'http://kuberay-apiserver-service.default.svc.cluster.local:8888/apis/v1/namespaces/kubeflow-ml/jobs' \
  //     -H 'accept: application/json' \
  //     -H 'Content-Type: application/json' \
  //     -d '{
  //     "name": "rayjob-test-long-running4",
  //     "namespace": "kubeflow-ml",
  //     "user": "3cp0",
  //     "version": "2.46.0",
  //     "entrypoint": "python -c \"import time; time.sleep(100)\"",
  //     "clusterSpec": {
  //       "headGroupSpec": {
  //         "computeTemplate": "default-template",
  //         "image": "rayproject/ray:2.46.0",
  //         "serviceType": "NodePort",
  //         "rayStartParams": {
  //           "dashboard-host": "0.0.0.0"
  //         },
  //         "labels": {
  //           "sidecar.istio.io/inject": "false"
  //         }
  //       },
  //       "workerGroupSpec": [
  //         {
  //           "groupName": "small-wg",
  //           "computeTemplate": "default-template",
  //           "image": "rayproject/ray:2.46.0",
  //           "replicas": 1,
  //           "minReplicas": 0,
  //           "maxReplicas": 1,
  //           "rayStartParams": {
  //             "metrics-export-port": "8080"
  //           },
  //           "labels": {
  //             "sidecar.istio.io/inject": "false"
  //           }
  //         }
  //       ]
  //     }
  //   }'
  const url = `${config.url}/namespaces/${namespace}/jobs`;
  const data = {
    name: jobName,
    namespace: "default",
    user: "steve-han",
    version: "2.46.0",
    entrypoint: entrypoint,
    shutdownAfterJobFinishes: true,
    ttlSecondsAfterFinished: 60,
    jobSubmitter: {
      image: "rayproject/ray:2.46.0",
    },
    clusterSpec: {
      headGroupSpec: {
        computeTemplate: "default-template",
        image: dockerImage,
        rayStartParams: {
          "dashboard-host": "0.0.0.0",
        },
      },
      workerGroupSpec: [
        {
          groupName: "small-wg",
          computeTemplate: "default-template",
          image: dockerImage,
          replicas: 1,
          minReplicas: 0,
          maxReplicas: 1,
          rayStartParams: {
            "metrics-export-port": "8080",
          },
        },
      ],
    },
  };

  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(data),
  });

  if (response.ok) {
    return await response.json();
  }
  console.log(response.status, response.statusText);
  const err = await response.json();
  throw new Error(err.message);

  // .then((response) => {
  //   if (response.ok) {
  //     return response.json();
  //   }
  //   return Promise.reject(response);
  // })
  // .then((data) => {
  //   console.log(data);
  //   return data;
  // })
  // .catch((response) => {
  //   console.log(response.status, response.statusText);
  //   response.json().then((json: any) => {
  //     return Promise.reject(json);
  //   });
  // });
}

export const useCreateJob = () => {
  const [creating, setCreating] = React.useState(false);
  const router = useRouter();
  const snackBar = useSnackBar();
  const namespace = useNamespace();

  const createJob = async (
    jobName: string,
    dockerImage: string,
    entrypoint: string
  ) => {
    setCreating(true);
    try {
      // Create the job and use "mutate" to force refresh the cache.
      // We don't want to use the result of creation to populate cache. Just
      // fetch from remote.
      await mutate(
        `/namespaces/${namespace}/jobs`,
        _createJob(namespace, jobName, dockerImage, entrypoint),
        { populateCache: false }
      );
      snackBar.showSnackBar(
        "Success",
        `Job ${jobName} created successfully`,
        "success"
      );
      setCreating(false);
      router.push("/jobs");
    } catch (err: any) {
      snackBar.showSnackBar(
        `Failed to create job ${jobName}.`,
        `Error: ${err}`,
        "danger"
      );
      setCreating(false);
    }
  };

  return {
    creating,
    createJob,
  };
};

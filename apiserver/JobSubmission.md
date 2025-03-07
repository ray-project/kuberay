# Job Submission using API server

Ray provides very convinient and powerful [Job Submission APIs]. The issue is that it needs to
access cluster's dashboard Ingress, which currently does not have any security implementations.
Alternatively you can use Ray cluster head service for this, but in this case your Ray job
management code has to run in the same kubernetes cluster as Ray. Job submission implementation in
the API server leverages already exposed URL of the API server to locate cluster and use its head
service to implement Job management functionality. Because you can access API server running on the
remote kubernetes cluster you can use these APIs for managing remote Ray clusters without exposing
them via Ingress.

## Using Job Submission APIs

Note that job submission APIs will only work if you are running API server within kubernetes
cluster. Local Development option of the API server will not work.

The first step is to deploy KubeRay operator and API server.

### Deploy KubeRay operator and API server

Reffer to [readme](README.md) for setting up KubRay operator and API server.

```shell
make operator-image cluster load-operator-image deploy-operator
```

Once they are set up, you first need to create a Ray cluster using the following commands:

### Deploy Ray cluster

Execute the following commands:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "default-template",
    "namespace": "default",
    "cpu": 2,
    "memory": 4
  }'

curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "test-cluster",
    "namespace": "default",
    "user": "boris",
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0-py310",
        "serviceType": "NodePort",
        "rayStartParams": {
          "dashboard-host": "0.0.0.0",
          "metrics-export-port": "8080"
        },
        "volumes": [
          {
            "name": "code-sample",
            "mountPath": "/home/ray/samples",
            "volumeType": "CONFIGMAP",
            "source": "ray-job-code-sample",
            "items": {"sample_code.py" : "sample_code.py"}
          }
        ]
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0-py310",
          "replicas": 1,
          "minReplicas": 1,
          "maxReplicas": 1,
          "rayStartParams": {
            "node-ip-address": "$MY_POD_IP"
          },
          "volumes": [
            {
              "name": "code-sample",
              "mountPath": "/home/ray/samples",
              "volumeType": "CONFIGMAP",
              "source": "ray-job-code-sample",
              "items": {"sample_code.py" : "sample_code.py"}
            }
          ]
        }
      ]
    }
  }'
```

Note that this cluster is mounting a volume from a configmap. This config map should be created
prior to cluster creation using [this YAML].

### Submit Ray Job

Once the cluster is up and running, you can submit a job to the cluster using the following command:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster' \
  --header 'Content-Type: application/json' \
  --data '{
    "entrypoint": "python /home/ray/samples/sample_code.py",
    "runtimeEnv": "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
    "numCpus": ".5"
  }'
```

This should return the following:

```json
{
  "submissionId":"raysubmit_KWZLwme56esG3Wcr"
}
```

Note that the `submissionId` value that you will get is different

### Get job details

Once the job is submitted, the following command can be used to get job's details. Note that
submission ID returned during job creation should be used here.

```shell
curl -X GET 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster/raysubmit_KWZLwme56esG3Wcr' \
  --header 'Content-Type: application/json'
```

This should return JSON similar to the one below

```json
{
   "entrypoint":"python /home/ray/samples/sample_code.py",
   "jobId":"02000000",
   "submissionId":"raysubmit_KWZLwme56esG3Wcr",
   "status":"SUCCEEDED",
   "message":"Job finished successfully.",
   "startTime":"1699442662879",
   "endTime":"1699442682405",
   "runtimeEnv":{
      "env_vars":"map[counter_name:test_counter]",
      "pip":"[requests==2.26.0 pendulum==2.1.2]"
   }
}
```

### Get Job log

You can also get job execution log using the following command. Note that submission ID returned
during job creation should be used here.

```shell
curl -X GET 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster/log/raysubmit_KWZLwme56esG3Wcr' \
  --header 'Content-Type: application/json'
```

This will return execution log, that will look something like the following

```text
2023-11-08 03:24:31,904\tINFO worker.py:1329 -- Using address 10.244.2.2:6379 set in the environment variable RAY_ADDRESS
2023-11-08 03:24:31,905\tINFO worker.py:1458 -- Connecting to existing Ray cluster at address: 10.244.2.2:6379...
2023-11-08 03:24:31,921\tINFO worker.py:1633 -- Connected to Ray cluster. View the dashboard at 10.244.2.2:8265
test_counter got 1
test_counter got 2
test_counter got 3
test_counter got 4
test_counter got 5
```

Note that this command always returns execution log from the begining (no streaming support) till
the current moment.

### List jobs

You can also list all the jobs (in any state) in the Ray cluster using the following command:

```shell
curl -X GET 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster' \
  --header 'Content-Type: application/json'
```

This should return the list of the submissions, that looks as follows:

```json
{
   "submissions":[
      {
         "entrypoint":"python /home/ray/samples/sample_code.py",
         "jobId":"02000000",
         "submissionId":"raysubmit_KWZLwme56esG3Wcr",
         "status":"SUCCEEDED",
         "message":"Job finished successfully.",
         "startTime":"1699442662879",
         "endTime":"1699442682405",
         "runtimeEnv":{
            "env_vars":"map[counter_name:test_counter]",
            "pip":"[requests==2.26.0 pendulum==2.1.2]"
         }
      }
   ]
}
```

### Stop Job

Execution of the job can be stoped using the following command. Note that submission ID returned
during job creation should be used here.

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster/raysubmit_KWZLwme56esG3Wcr' \
  --header 'Content-Type: application/json'
```

### Delete Job

Finally, you can delete job using the following command. Note that submission ID returned during job
creation should be used here.

```shell
curl -X DELETE 'localhost:31888/apis/v1/namespaces/default/jobsubmissions/test-cluster/raysubmit_KWZLwme56esG3Wcr' \
  --header 'Content-Type: application/json'
```

You can validate job deletion by looking at the Ray dashboard (jobs pane) and ensuring that it was removed

[Job Submission APIs]: https://docs.ray.io/en/latest/cluster/running-applications/job-submission/rest.html
[this YAML]: /test/job/code.yaml

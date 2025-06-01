let jobs = [
  {
    name: "	Embedding all of Roblox's data",
    namespace: "ray-system",
    user: "3cp0",
    entrypoint: "python -V",
    jobId: "rayjob-test-drhlq",
    clusterSpec: {
      headGroupSpec: {
        computeTemplate: "default-template",
        image: "rayproject/ray:2.46.0",
        serviceType: "NodePort",
        rayStartParams: {
          "dashboard-host": "0.0.0.0",
        },
      },
      workerGroupSpec: [
        {
          groupName: "small-wg",
          computeTemplate: "default-template",
          image: "rayproject/ray:2.46.0",
          replicas: 1,
          minReplicas: 1,
          maxReplicas: 1,
          rayStartParams: {
            "metrics-export-port": "8080",
          },
        },
      ],
    },
    createdAt: "2024-03-25T21:36:02Z",
    jobStatus: "PENDING",
    jobDeploymentStatus: "Running",
    message: "Job being scheduled.",
  },
  {
    name: "rayjob-test",
    namespace: "ray-system",
    user: "3cp0",
    entrypoint: "python -V",
    jobId: "rayjob-test-drhlq",
    clusterSpec: {
      headGroupSpec: {
        computeTemplate: "default-template",
        image: "rayproject/ray:2.46.0",
        serviceType: "NodePort",
        rayStartParams: {
          "dashboard-host": "0.0.0.0",
        },
      },
      workerGroupSpec: [
        {
          groupName: "small-wg",
          computeTemplate: "default-template",
          image: "rayproject/ray:2.46.0",
          replicas: 1,
          minReplicas: 1,
          maxReplicas: 1,
          rayStartParams: {
            "metrics-export-port": "8080",
          },
        },
      ],
    },
    createdAt: "2023-09-25T11:36:02Z",
    jobStatus: "SUCCEEDED",
    jobDeploymentStatus: "Running",
    message: "Job finished successfully.",
  },
  {
    name: "rayjob-test-2",
    namespace: "ray-system",
    user: "3cp0",
    entrypoint: "python -V",
    jobId: "rayjob-test-drhlq",
    clusterSpec: {
      headGroupSpec: {
        computeTemplate: "default-template",
        image: "rayproject/ray:2.46.0",
        serviceType: "NodePort",
        rayStartParams: {
          "dashboard-host": "0.0.0.0",
        },
      },
      workerGroupSpec: [
        {
          groupName: "small-wg",
          computeTemplate: "default-template",
          image: "rayproject/ray:2.46.0",
          replicas: 1,
          minReplicas: 1,
          maxReplicas: 1,
          rayStartParams: {
            "metrics-export-port": "8080",
          },
        },
      ],
    },
    createdAt: "2023-09-25T12:36:02Z",
    jobStatus: "FAILED",
    jobDeploymentStatus: "Running",
    message: "Task Failed Successfully",
  },
  {
    name: "Dan's personal Ray Job",
    namespace: "ray-system",
    user: "3cp0",
    entrypoint: "python -V",
    jobId: "rayjob-test-drhlq",
    clusterSpec: {
      headGroupSpec: {
        computeTemplate: "default-template",
        image: "rayproject/ray:2.46.0",
        serviceType: "NodePort",
        rayStartParams: {
          "dashboard-host": "0.0.0.0",
        },
      },
      workerGroupSpec: [
        {
          groupName: "small-wg",
          computeTemplate: "default-template",
          image: "rayproject/ray:2.46.0",
          replicas: 1,
          minReplicas: 1,
          maxReplicas: 1,
          rayStartParams: {
            "metrics-export-port": "8080",
          },
        },
      ],
    },
    createdAt: "2023-09-28T12:36:02Z",
    jobStatus: "RUNNING",
    jobDeploymentStatus: "Running",
    message: "Resource Quota exceeded: requested 350,000 H100 GPUs, the cluster has 1 available. ",
  },
];

for (let i = 0; i < 100; i++) {
  jobs.push({ ...jobs[1], name: "rayjob-test-generated-" + i });
}

export {jobs};

import json
from python_apiserver_client import *


def test_toleration():

    tol1 = Toleration(key="blah1", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute)
    print(f"\ntoleration 1: {tol1.to_string()}")
    t1_json = json.dumps(tol1.to_dict())
    print(f"toleration 1 JSON: {t1_json}")

    tol2 = Toleration(key="blah2", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute,
                      value="value")
    print(f"toleration 2: {tol2.to_string()}")
    t2_json = json.dumps(tol2.to_dict())
    print(f"toleration 2 JSON: {t2_json}")

    assert tol1.to_string() == toleration_decoder(json.loads(t1_json)).to_string()
    assert tol2.to_string() == toleration_decoder(json.loads(t2_json)).to_string()


def test_templates():

    tol1 = Toleration(key="blah1", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute)
    tol2 = Toleration(key="blah2", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute,
                      value="value")

    temp1 = Template(name="template1", namespace="namespace", cpu=1, memory=4, tolerations=[tol1, tol2])
    print(f"\ntemplate 1: {temp1.to_string()}")
    tm1_json = json.dumps(temp1.to_dict())
    print(f"template 1 JSON: {tm1_json}")

    temp2 = Template(name="template2", namespace="namespace", cpu=2, memory=8, gpu=1)
    print(f"template 2: {temp2.to_string()}")
    tm2_json = json.dumps(temp2.to_dict())
    print(f"template 2 JSON: {tm2_json}")

    assert temp1.to_string() == template_decoder(json.loads(tm1_json)).to_string()
    assert temp2.to_string() == template_decoder(json.loads(tm2_json)).to_string()


def test_volumes():

    # hostPath
    vol = HostPathVolume(name="hostPath", mount_path="tmp/hostPath", source="source",
                         hostpathtype=HostPath.FILE, mountpropagation=MountPropagationMode.NONE)
    print(f"\nhostPath volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"host path volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()

    vol = PVCVolume(name="pvc", mount_path="tmp/pvc", source="claim", read_only=True,
                    mountpropagation=MountPropagationMode.BIDIRECTIONAL)
    print(f"PVC volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"PVC volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()

    vol = EphemeralVolume(name="ephemeral", mount_path="tmp/ephemeral", storage="5Gi", storage_class="blah",
                          accessmode=AccessMode.RWX)
    print(f"Ephemeral volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"Ephemeral volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()

    vol = EmptyDirVolume(name="emptyDir", mount_path="tmp/emptyDir")
    print(f"Empty dir volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"Empty dir volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()

    vol = ConfigMapVolume(name="confmap", mount_path="tmp/confmap", source="my-map",
                          items={"sample_code.py": "sample_code.py"})
    print(f"config map volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"config map volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()

    vol = SecretVolume(name="secret", mount_path="tmp/secret", source="my-secret")
    print(f"secret volume: {vol.to_string()}")
    vol_json = json.dumps(vol.to_dict())
    print(f"secret volume json: {vol_json}")
    assert volume_decoder(json.loads(vol_json)).to_string() == vol.to_string()


def test_environment():

    env_v = EnvVarFrom(source=EnvarSource.SECRET, name="my-secret", key="key")
    print(f"\nEnv variable from: {env_v.to_string()}")
    env_v_json = json.dumps(env_v.to_dict())
    print(f"Env variable from JSON: {env_v_json}")
    assert envvarfrom_decoder(json.loads(env_v_json)).to_string() == env_v.to_string()

    envs = EnvironmentVariables(keyvalue={"key": "val"}, fromref={"key_ref": env_v})
    print(f"Env variables: {envs.to_string()}")
    envs_json = json.dumps(envs.to_dict())
    print(f"Env variables JSON: {envs_json}")
    assert environmentvariables_decoder(json.loads(envs_json)).to_string() == envs.to_string()

    envs = EnvironmentVariables(fromref={"key_ref": env_v})
    print(f"Env variables: {envs.to_string()}")
    envs_json = json.dumps(envs.to_dict())
    print(f"Env variables JSON: {envs_json}")
    assert environmentvariables_decoder(json.loads(envs_json)).to_string() == envs.to_string()

    envs = EnvironmentVariables(keyvalue={"key": "val"})
    print(f"Env variables: {envs.to_string()}")
    envs_json = json.dumps(envs.to_dict())
    print(f"Env variables JSON: {envs_json}")
    assert environmentvariables_decoder(json.loads(envs_json)).to_string() == envs.to_string()


def test_head_node_spec():

    env_v = EnvVarFrom(source=EnvarSource.SECRET, name="my-secret", key="key")
    env_s = EnvironmentVariables(keyvalue={"key": "val"}, fromref={"key_ref": env_v})
    volumes = [PVCVolume(name="pvc", mount_path="tmp/pvc", source="claim", read_only=True,
                         mountpropagation=MountPropagationMode.BIDIRECTIONAL),
               EmptyDirVolume(name="emptyDir", mount_path="tmp/emptyDir")]

    head = HeadNodeSpec(compute_template="template", ray_start_params=DEFAULT_HEAD_START_PARAMS,
                        enable_ingress=True, service_type=ServiceType.ClusterIP, volumes=volumes,
                        environment=env_s)
    print(f"\nhead node: {head.to_string()}")
    head_json = json.dumps(head.to_dict())
    print(f"head node JSON: {head_json}")
    assert head_node_spec_decoder(json.loads(head_json)).to_string() == head.to_string()


def test_worker_node_spec():

    env_v = EnvVarFrom(source=EnvarSource.SECRET, name="my-secret", key="key")
    env_s = EnvironmentVariables(keyvalue={"key": "val"}, fromref={"key_ref": env_v})
    volumes = [PVCVolume(name="pvc", mount_path="tmp/pvc", source="claim", read_only=True,
                         mountpropagation=MountPropagationMode.BIDIRECTIONAL),
               EmptyDirVolume(name="emptyDir", mount_path="tmp/emptyDir")]

    worker = WorkerNodeSpec(group_name="group", compute_template="template", replicas=2, min_replicas=2,
                            max_replicas=2, ray_start_params=DEFAULT_WORKER_START_PARAMS, volumes=volumes,
                            environment=env_s, labels={"key": "value"})
    print(f"\nworker node: {worker.to_string()}")
    worker_json = json.dumps(worker.to_dict())
    print(f"worker node JSON: {worker_json}")
    assert worker_node_spec_decoder(json.loads(worker_json)).to_string() == worker.to_string()


def test_cluster_spec():
    env_s = EnvironmentVariables(keyvalue={"key": "val"},
                                 fromref={"key_ref": EnvVarFrom(source=EnvarSource.SECRET,
                                                                name="my-secret", key="key")})
    volumes = [PVCVolume(name="pvc", mount_path="tmp/pvc", source="claim", read_only=True,
                         mountpropagation=MountPropagationMode.BIDIRECTIONAL),
               EmptyDirVolume(name="emptyDir", mount_path="tmp/emptyDir")]
    spec = ClusterSpec(head_node=HeadNodeSpec(compute_template="template", ray_start_params=DEFAULT_HEAD_START_PARAMS,
                                              enable_ingress=True, service_type=ServiceType.ClusterIP, volumes=volumes,
                                              environment=env_s),
                       worker_groups=[WorkerNodeSpec(group_name="group", compute_template="template", replicas=2,
                                                     min_replicas=2, max_replicas=2,
                                                     ray_start_params=DEFAULT_WORKER_START_PARAMS, volumes=volumes,
                                                     environment=env_s, labels={"key": "value"}),
                                      WorkerNodeSpec(group_name="group1", compute_template="template1", replicas=2,
                                                     min_replicas=2, max_replicas=2,
                                                     ray_start_params=DEFAULT_WORKER_START_PARAMS, volumes=volumes,
                                                     environment=env_s, labels={"key": "value"})])
    print(f"\ncluster spec: {spec.to_string()}")
    spec_json = json.dumps(spec.to_dict())
    print(f"cluster spec JSON: {spec_json}")
    assert cluster_spec_decoder(json.loads(spec_json)).to_string() == spec.to_string()


def test_cluster():

    event = {"id": "id", "name": "name", "created_at": "ts", "first_timestamp": "ts", "last_timestamp": "ts",
             "reason": "reason", "message": "message", "type": "warning", "count": "1"}
    print(f"\ncluster event: {ClusterEvent(event).to_string()}")
    env_s = EnvironmentVariables(keyvalue={"key": "val"},
                                 fromref={"key_ref": EnvVarFrom(source=EnvarSource.SECRET, name="my-secret",
                                                                key="key")})
    volumes = [PVCVolume(name="pvc", mount_path="tmp/pvc", source="claim", read_only=True,
                         mountpropagation=MountPropagationMode.BIDIRECTIONAL),
               EmptyDirVolume(name="emptyDir", mount_path="tmp/emptyDir")]
    spec = ClusterSpec(head_node=HeadNodeSpec(compute_template="template", ray_start_params=DEFAULT_HEAD_START_PARAMS,
                                              enable_ingress=True, service_type=ServiceType.ClusterIP, volumes=volumes,
                                              environment=env_s, annotations={"a_key": "a_val"}),
                       worker_groups=[WorkerNodeSpec(group_name="group", compute_template="template", replicas=2,
                                                     min_replicas=2, max_replicas=2,
                                                     ray_start_params=DEFAULT_WORKER_START_PARAMS, volumes=volumes,
                                                     environment=env_s, labels={"key": "value"}),
                                      WorkerNodeSpec(group_name="group1", compute_template="template1", replicas=2,
                                                     min_replicas=2, max_replicas=2,
                                                     ray_start_params=DEFAULT_WORKER_START_PARAMS, volumes=volumes,
                                                     environment=env_s, labels={"key": "value"})])
    cluster = Cluster(name="test", namespace="default", user="boris", version="2.9.0", cluster_spec=spec,
                      deployment_environment=Environment.DEV, cluster_environment=env_s)
    print(f"cluster: {cluster.to_string()}")
    cluster_json = json.dumps(cluster.to_dict())
    print(f"cluster JSON: {cluster_json}")
    assert cluster_decoder(json.loads(cluster_json)).to_string() == cluster.to_string()

    cluster_dict = cluster.to_dict()
    cluster_dict["created_at"] = "created"
    cluster_dict["created_status"] = "status"
    cluster_dict["events"] = [event]
    print(f"cluster with output: {cluster_decoder(cluster_dict).to_string()}")


def test_submission():
    yaml = """
    pip:
      - requests==2.26.0
      - pendulum==2.1.2
    env_vars:
      counter_name: test_counter    
    """
    request = RayJobRequest(entrypoint="python /home/ray/samples/sample_code.py",
                            runtime_env=yaml, num_cpu=.5)
    print(f"job request: {request.to_string()}")
    request_json = json.dumps(request.to_dict())
    print(f"request JSON: {request_json}")

    infoJson = """
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
    """
    job_info = RayJobInfo(json.loads(infoJson))
    print(job_info.to_string())

def test_serve():
    json_string = """{
   "deployMode":"MULTI_APP",
   "proxyLocation":"EveryNode",
   "controllerInfo":{
      "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
      "nodeIp":"10.244.1.2",
      "actorId":"ceac9504c1ce994f152afcbe01000000",
      "actorName":"SERVE_CONTROLLER_ACTOR",
      "logFilePath":"/serve/controller_451.log"
   },
   "httpOptions":{
      "host":"0.0.0.0",
      "port":8000,
      "keepAliveTimeoutS":5
   },
   "grpcOptions":{
      "port":9000
   },
   "applications":{
      "fruit_app":{
         "name":"fruit_app",
         "status":"RUNNING",
         "routePrefix":"/fruit",
         "lastDeployedTimeS":1702216400,
         "deployments":{
            "DAGDriver":{
               "name":"DAGDriver",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"b9ea9613d134735b1a42f5bb01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#DAGDriver#HhnVXp",
                     "workerId":"bc1aa743dc38799fe5314192c4b82efcbf6b3dd2473b062ac15cc376",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_DAGDriver_fruit_app#DAGDriver#HhnVXp.log",
                     "replicaId":"fruit_app#DAGDriver#HhnVXp",
                     "pid":249,
                     "startTimeS":1702216400
                  }
               ]
            },
            "FruitMarket":{
               "name":"FruitMarket",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"FruitMarket",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"95e00bd164c069eaced206c801000000",
                     "actorName":"SERVE_REPLICA::fruit_app#FruitMarket#vMilEM",
                     "workerId":"6eb5f8ead62db87f088a7e3c13211c442c5344e431905a556052ebc7",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_FruitMarket_fruit_app#FruitMarket#vMilEM.log",
                     "replicaId":"fruit_app#FruitMarket#vMilEM",
                     "pid":613,
                     "startTimeS":1702216400
                  }
               ]
            },
            "MangoStand":{
               "name":"MangoStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"MangoStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"3"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"d80b934549a6155716d706d701000000",
                     "actorName":"SERVE_REPLICA::fruit_app#MangoStand#MbsqaU",
                     "workerId":"af8dbb9355cf00e52212dfc8309f240dd0147119319829ea2801eeb5",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_MangoStand_fruit_app#MangoStand#MbsqaU.log",
                     "replicaId":"fruit_app#MangoStand#MbsqaU",
                     "pid":199,
                     "startTimeS":1702216400
                  }
               ]
            },
            "OrangeStand":{
               "name":"OrangeStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"OrangeStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"2"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"b3fe6a18933f73bab4028f8801000000",
                     "actorName":"SERVE_REPLICA::fruit_app#OrangeStand#duLXfz",
                     "workerId":"ca5859275605da35ee5b4aea595a05b3311e75a71abb99923bb8831b",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_OrangeStand_fruit_app#OrangeStand#duLXfz.log",
                     "replicaId":"fruit_app#OrangeStand#duLXfz",
                     "pid":612,
                     "startTimeS":1702216400
                  }
               ]
            },
            "PearStand":{
               "name":"PearStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"PearStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"1"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"f36536b0dca1fe7cc6ecc6de01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#PearStand#MdjHOm",
                     "workerId":"fd0519e76dc5728c8a9b6f940fe59eaba2dc634d17b700de2166110b",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_PearStand_fruit_app#PearStand#MdjHOm.log",
                     "replicaId":"fruit_app#PearStand#MdjHOm",
                     "pid":200,
                     "startTimeS":1702216400
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"fruit_app",
            "routePrefix":"/fruit",
            "importPath":"fruit.deployment_graph",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
            },
            "deployments":[
               {
                  "name":"MangoStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"3"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"OrangeStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"2"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"PearStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"1"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"FruitMarket",
                  "numReplicas":1,
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               }
            ]
         }
      },
      "math_app":{
         "name":"math_app",
         "status":"RUNNING",
         "routePrefix":"/calc",
         "lastDeployedTimeS":1702216400,
         "deployments":{
            "Adder":{
               "name":"Adder",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Adder",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "increment":"3"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"6838547ea22fbcf566d2057201000000",
                     "actorName":"SERVE_REPLICA::math_app#Adder#ZtvAeP",
                     "workerId":"e1b2d156136dec836ae78935043e8e9aa0d175b2ea5ddd2ac4839a2c",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Adder_math_app#Adder#ZtvAeP.log",
                     "replicaId":"math_app#Adder#ZtvAeP",
                     "pid":250,
                     "startTimeS":1702216400
                  }
               ]
            },
            "DAGDriver":{
               "name":"DAGDriver",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"7149d5935b9088cb030fb3ba01000000",
                     "actorName":"SERVE_REPLICA::math_app#DAGDriver#LBkgea",
                     "workerId":"ddc782bd94d10608b59bc31f12240a01df29c0b5814ca6499010de7c",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_DAGDriver_math_app#DAGDriver#LBkgea.log",
                     "replicaId":"math_app#DAGDriver#LBkgea",
                     "pid":718,
                     "startTimeS":1702216400
                  }
               ]
            },
            "Multiplier":{
               "name":"Multiplier",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Multiplier",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "factor":"5"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"303757bcd36f8dae1c6bbc8701000000",
                     "actorName":"SERVE_REPLICA::math_app#Multiplier#MCQNmH",
                     "workerId":"184f5a9d39e19e001a27818f7b4e31645c094066dfbb2aa0d4d34171",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Multiplier_math_app#Multiplier#MCQNmH.log",
                     "replicaId":"math_app#Multiplier#MCQNmH",
                     "pid":638,
                     "startTimeS":1702216400
                  }
               ]
            },
            "Router":{
               "name":"Router",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Router",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"54724715f2e3ee487b42852401000000",
                     "actorName":"SERVE_REPLICA::math_app#Router#uezNSp",
                     "workerId":"9bc286583533cd9f1bc429067a3a130ee810bedbb913ac62d0b1a102",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Router_math_app#Router#uezNSp.log",
                     "replicaId":"math_app#Router#uezNSp",
                     "pid":663,
                     "startTimeS":1702216400
                  }
               ]
            },
            "create_order":{
               "name":"create_order",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"create_order",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"0a29af7ba1777655ed63d6b701000000",
                     "actorName":"SERVE_REPLICA::math_app#create_order#IadAbU",
                     "workerId":"9c452e868e05f16ad10d71b92d76f1285efaccb6bf902238c7e4fc9f",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_create_order_math_app#create_order#IadAbU.log",
                     "replicaId":"math_app#create_order#IadAbU",
                     "pid":328,
                     "startTimeS":1702216400
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"math_app",
            "routePrefix":"/calc",
            "importPath":"conditional_dag.serve_dag",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
            },
            "deployments":[
               {
                  "name":"Adder",
                  "numReplicas":1,
                  "userConfig":{
                     "increment":"3"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"Multiplier",
                  "numReplicas":1,
                  "userConfig":{
                     "factor":"5"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"Router",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               },
               {
                  "name":"create_order",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               },
               {
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               }
            ]
         }
      }
   },
   "proxies":{
      "4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba":{
         "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
         "nodeIp":"10.244.2.3",
         "actorId":"985d4e1b1115bfd8aadfcd5201000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
         "workerId":"3ca22e41adcba21db3f155c344945306c600a0db994f3e8d9349c523",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.2.3.log"
      },
      "e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26":{
         "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
         "nodeIp":"10.244.1.2",
         "actorId":"a6a37816bca13d25d88a900201000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
         "workerId":"d8ea829af0423c1dcf640d4531c284ab5b2f74b6ec764a6e75d64f98",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.1.2.log"
      }
   }
}
"""

    json_d = json.loads(json_string)
    serve = ServeInstance(json_d)
    print(serve.to_string())

import time

from python_apiserver_client import *

def test_templates():
    apis = KubeRayAPIs(token="12345")
    # create
    toleration = Toleration(key="blah1", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute)
    template = Template(name="test-template", namespace="default", cpu=2, memory=8, tolerations=[toleration])
    status, error = apis.create_compute_template(template)
    assert status == 200
    assert error is None
    # duplicate create should fail
    status, error = apis.create_compute_template(template)
    assert status != 200
    assert error is not None
    print(f"\nstatus {status}, error code: {str(error)}")
    # get
    status, error, t = apis.get_compute_template(ns="default", name="test-template")
    assert status == 200
    assert error is None
    assert template.to_string() == t.to_string()
    # list
    status, error, templates = apis.list_compute_templates()
    assert status == 200
    assert error is None
    assert template.to_string() == templates[0].to_string()
    # list ns
    status, error, templates = apis.list_compute_templates_namespace(ns="default")
    assert status == 200
    assert error is None
    assert template.to_string() == templates[0].to_string()
    # delete
    status, error = apis.delete_compute_template(ns="default", name="test-template")
    assert status == 200
    assert error is None
    # duplicate delete should fail
    status, error = apis.delete_compute_template(ns="default", name="test-template")
    assert status != 200
    assert error is not None
    print(f"status: {status}, err = {str(error)}")


def test_cluster():
    apis = KubeRayAPIs(token="12345")
    # Create template first
    template = Template(name="default-template", namespace="default", cpu=2, memory=4)
    status, error = apis.create_compute_template(template)
    assert status == 200
    assert error is None
    # cluster
    volume = ConfigMapVolume(name="code-sample", mount_path="/home/ray/samples", source="ray-job-code-sample",
                             items={"sample_code.py": "sample_code.py"})
    environment = EnvironmentVariables(keyvalue={"key": "value"})
    head = HeadNodeSpec(compute_template="default-template",
                        ray_start_params={"metrics-export-port": "8080", "num-cpus": "0"},
                        image="rayproject/ray:2.9.0-py310", service_type=ServiceType.ClusterIP,
                        volumes=[volume], environment=environment)
    worker = WorkerNodeSpec(group_name="small", compute_template="default-template", replicas=1,
                            min_replicas=1, max_replicas=1, ray_start_params=DEFAULT_WORKER_START_PARAMS,
                            image="rayproject/ray:2.9.0-py310", volumes=[volume], environment=environment)
    cluster = Cluster(name="test", namespace="default", user="boris", version="2.9.0",
                      cluster_spec=ClusterSpec(head_node=head, worker_groups=[worker]))
    # create
    status, error = apis.create_cluster(cluster)
    assert status == 200
    assert error is None
    # get
    status, error, c = apis.get_cluster(ns="default", name="test")
    assert status == 200
    assert error is None
    print(f"\ngot cluster: {c.to_string()}")
    # list
    status, error, clusters = apis.list_clusters()
    assert status == 200
    assert error is None
    assert len(clusters) == 1
    print(f"got cluster: {clusters[0].to_string()}")
    # list namespace
    status, error, clusters = apis.list_clusters_namespace(ns="default")
    assert status == 200
    assert error is None
    assert len(clusters) == 1
    print(f"got cluster: {clusters[0].to_string()}")
    # get cluster status
    status, error, cs = apis.get_cluster_status(ns="default", name="test")
    assert status == 200
    assert error is None
    print(f"cluster status is {cs}")
    # Wait for the cluster to get ready
    status, error = apis.wait_cluster_ready(ns="default", name="test")
    assert status == 200
    assert error is None
    # get endpoints
    status, error, endpoint = apis.get_cluster_endpoints(ns="default", name="test")
    assert status == 200
    assert error is None
    print(f"cluster endpoints is {endpoint}")
    # delete cluster
    status, error = apis.delete_cluster(ns="default", name="test")
    assert status == 200
    assert error is None
    # delete template
    status, error = apis.delete_compute_template(ns="default", name="default-template")
    assert status == 200
    assert error is None

def test_job_submission():
    apis = KubeRayAPIs()
    # Create template first
    template = Template(name="default-template", namespace="default", cpu=2, memory=4)
    status, error = apis.create_compute_template(template)
    assert status == 200
    assert error is None
    # cluster
    volume = ConfigMapVolume(name="code-sample", mount_path="/home/ray/samples", source="ray-job-code-sample",
                             items={"sample_code.py": "sample_code.py"})
    environment = EnvironmentVariables(keyvalue={"key": "value"})
    head = HeadNodeSpec(compute_template="default-template",
                        ray_start_params={"metrics-export-port": "8080", "num-cpus": "0"},
                        image="rayproject/ray:2.9.0-py310", service_type=ServiceType.ClusterIP,
                        volumes=[volume], environment=environment)
    worker = WorkerNodeSpec(group_name="small", compute_template="default-template", replicas=1,
                            min_replicas=1, max_replicas=1, ray_start_params=DEFAULT_WORKER_START_PARAMS,
                            image="rayproject/ray:2.9.0-py310", volumes=[volume], environment=environment)
    cluster = Cluster(name="test-job", namespace="default", user="boris", version="2.9.0",
                      cluster_spec=ClusterSpec(head_node=head, worker_groups=[worker]))
    # create
    status, error = apis.create_cluster(cluster)
    assert status == 200
    assert error is None
    # Wait for the cluster to get ready
    status, error = apis.wait_cluster_ready(ns="default", name="test-job")
    assert status == 200
    assert error is None
    # submit Ray job
    resource_yaml = """
    pip:
      - requests==2.26.0
      - pendulum==2.1.2
    env_vars:
      counter_name: test_counter
    """
    jobRequest = RayJobRequest(entrypoint="python /home/ray/samples/sample_code.py",
                               runtime_env=resource_yaml, num_cpu=.5)
    status, error, sid = apis.submit_job(ns="default", name="test-job", jobrequest=jobRequest)
    assert status == 200
    assert error is None
    # get Ray job info
    status, error, jinfo = apis.get_job_info(ns="default", name="test-job", sid=sid)
    assert status == 200
    assert error is None
    print(f"\njobs info {jinfo.to_string()}")
    # get Ray jobs info
    status, error, jinfos = apis.list_job_info(ns="default", name="test-job")
    assert status == 200
    assert error is None
    print("jobs info")
    for inf in jinfos:
        print(f"    {inf.to_string()}")
    # get Ray job log
    time.sleep(5)  # wait till log is available
    status, error, jlog = apis.get_job_log(ns="default", name="test-job", sid=sid)
    assert status == 200
    assert error is None
    print(f"job log {jlog}")
    # stop Ray job
    status, error = apis.stop_ray_job(ns="default", name="test-job", sid=sid)
    assert status == 200
    assert error is None
    # delete Ray job
    status, error = apis.delete_ray_job(ns="default", name="test-job", sid=sid)
    assert status == 200
    assert error is None
    # delete cluster
    status, error = apis.delete_cluster(ns="default", name="test-job")
    assert status == 200
    assert error is None
    # delete template
    status, error = apis.delete_compute_template(ns="default", name="default-template")
    assert status == 200
    assert error is None

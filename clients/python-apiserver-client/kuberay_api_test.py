from python_client.kuberay_apis import *

from python_client.params.templates import *
from python_client.params.cluster import *
from python_client.params.headnode import *
from python_client.params.workernode import *
from python_client.params.volumes import *
from python_client.params.environmentvariables import *


def test_templates():
    apis = KubeRayAPIs()
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
                        image="rayproject/ray:2.7.0-py310", service_type=ServiceType.ClusterIP,
                        volumes=[volume], environment=environment)
    worker = WorkerNodeSpec(group_name="small", compute_template="default-template", replicas=1,
                            min_replicas=1, max_replicas=1, ray_start_params=DEFAULT_WORKER_START_PARAMS,
                            image="rayproject/ray:2.7.0-py310", volumes=[volume], environment=environment)
    cluster = Cluster(name="test", namespace="default", user="boris", version="2.7.0",
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
    # delete
    status, error = apis.delete_cluster(ns="default", name="test")
    assert status == 200
    assert error is None
    # delete template
    status, error = apis.delete_compute_template(ns="default", name="default-template")
    assert status == 200
    assert error is None

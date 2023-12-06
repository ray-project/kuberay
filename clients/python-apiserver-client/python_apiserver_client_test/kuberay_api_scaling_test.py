from python_apiserver_client import *
import time

def test_templates():
    apis = KubeRayAPIs()
    print()
    start = time.time()
    # create
    toleration = Toleration(key="blah1", operator=TolerationOperation.Exists, effect=TolerationEffect.NoExecute)
    template_base_name = "test-template"
    namespaces = ["default", "test1", "test2", "test3"]
    ns_index = 0
    n_templates = 100
    for i in range(n_templates):
        tname = f"{template_base_name}-{i}"
        template = Template(name=tname, namespace=namespaces[ns_index], cpu=2, memory=8, tolerations=[toleration])
        status, error = apis.create_compute_template(template)
        assert status == 200
        assert error is None
        ns_index += 1
        if ns_index >= len(namespaces):
            ns_index = 0
#        print(f"template {tname} is created")
    print(f"created {n_templates} templates in {time.time() - start} sec")
    start = time.time()
    # list for all ns
    status, error, templates = apis.list_compute_templates()
    assert status == 200
    assert error is None
    print(f"listed {len(templates)} templates in {time.time() - start} sec")
    start = time.time()
    # list for individual ns
    for ns in namespaces:
        status, error, templates = apis.list_compute_templates_namespace(ns=ns)
        assert status == 200
        assert error is None
        print(f"listed {len(templates)} templates in {ns} ns")
    print(f"listed templates from individual ns in {time.time() - start} sec")
    start = time.time()
    # delete
    ns_index = 0
    for i in range(n_templates):
        tname = f"{template_base_name}-{i}"
        status, error = apis.delete_compute_template(ns=namespaces[ns_index], name=tname)
        assert status == 200
        assert error is None
        ns_index += 1
        if ns_index >= len(namespaces):
            ns_index = 0
#        print(f"template {tname} is deleted")
    print(f"Deleted {n_templates} templates in {time.time() - start} sec")

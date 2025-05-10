from python_apiserver_client import *

apis = KubeRayAPIs(
    base="http://kuberay-apiserver-service.default.svc.cluster.local:8888"
)
template = Template(name="test-template", namespace="default", cpu=2, memory=8)
status, error = apis.create_compute_template(template)

print(status)
print(error)

status, error, templates = apis.list_compute_templates_namespace(ns="default")

print(template.to_string())

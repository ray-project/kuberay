"""Create RayCluster CR periodically"""
from string import Template
from datetime import datetime

import subprocess
import tempfile
import time

def create_ray_cluster(template, cr_name):
    """Replace the template with the name of the RayCluster CR and create the CR"""
    now = datetime.now()
    current_time = now.strftime("%H:%M:%S")
    print(f"Current Time = {current_time}, RayCluster CR: {cr_name} is created")

    with open(template, encoding="utf-8") as ray_cluster_template:
        template = Template(ray_cluster_template.read())
        yamlfile = template.substitute(
            {'raycluster_name': cr_name}
        )
    with tempfile.NamedTemporaryFile('w', suffix = '_ray_cluster_yaml') as ray_cluster_yaml:
        ray_cluster_yaml.write(yamlfile)
        ray_cluster_yaml.flush()
        # Execute a command "kubectl apply -f $ray_cluster_yaml.name"
        command = f"kubectl apply -f {ray_cluster_yaml.name}"
        subprocess.run(command, shell=True, check=False)

for i in range(100):
    create_ray_cluster("ray-cluster.benchmark.yaml.template", f"raycluster-{i}")
    subprocess.run("kubectl get raycluster", shell=True, check=False)
    time.sleep(60)

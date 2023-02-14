import sys
import os
from os import path


""" 
in case you are working directly with the source, and don't wish to 
install the module with pip install, you can directly import the packages by uncommenting the following code.
"""

"""
sys.path.append(path.dirname(path.dirname(path.abspath(__file__))))

current_dir = os.path.dirname(os.path.abspath(__file__))
parent_dir = os.path.abspath(os.path.join(current_dir, os.pardir))
sibling_dirs = [
    d for d in os.listdir(parent_dir) if os.path.isdir(os.path.join(parent_dir, d))
]
for sibling_dir in sibling_dirs:
    sys.path.append(os.path.join(parent_dir, sibling_dir))
"""

import kuberay_cluster_api

from utils import kuberay_cluster_utils, kuberay_cluster_builder


def main():

    print("starting cluster handler...")
    my_kuberay_api = kuberay_cluster_api.RayClusterApi()

    my_cluster_director = kuberay_cluster_builder.Director()

    my_cluster_builder = kuberay_cluster_builder.ClusterBuilder()

    my_Cluster_utils = kuberay_cluster_utils.ClusterUtils()

    cluster0 = my_cluster_director.build_small_cluster(name="new-cluster0")

    if cluster0:
        my_kuberay_api.create_ray_cluster(body=cluster0)

    cluster1 = (
        my_cluster_builder.build_meta(name="new-cluster1")
        .build_head()
        .build_worker(group_name="workers")
        .get_cluster()
    )

    if not my_cluster_builder.succeeded:
        print("error building the cluster, aborting...")
        return
    my_kuberay_api.create_ray_cluster(body=cluster1)

    cluster2 = (
        my_cluster_builder.build_meta(name="new-cluster2")
        .build_head()
        .build_worker(group_name="workers")
        .get_cluster()
    )

    if not my_cluster_builder.succeeded:
        print("error building the cluster, aborting...")
        return

    my_kuberay_api.create_ray_cluster(body=cluster2)

    cluster_to_patch, succeeded = my_Cluster_utils.update_worker_group_replicas(
        cluster2, group_name="workers", max_replicas=4, min_replicas=1, replicas=2
    )

    if succeeded:
        print(
            "trying to patch raycluster = {}".format(
                cluster_to_patch["metadata"]["name"]
            )
        )
        my_kuberay_api.patch_ray_cluster(
            name=cluster_to_patch["metadata"]["name"], ray_patch=cluster_to_patch
        )

    cluster_to_patch, succeeded = my_Cluster_utils.duplicate_worker_group(
        cluster1, group_name="workers", new_group_name="new-workers"
    )
    if succeeded:
        print(
            "trying to patch raycluster = {}".format(
                cluster_to_patch["metadata"]["name"]
            )
        )
        my_kuberay_api.patch_ray_cluster(
            name=cluster_to_patch["metadata"]["name"], ray_patch=cluster_to_patch
        )

    kube_ray_list = my_kuberay_api.list_ray_clusters(k8s_namespace="default")
    if "items" in kube_ray_list:
        line = "-" * 72
        print(line)
        print("{:<63s}{:>2s}".format("Name", "Namespace"))
        print(line)
        for cluster in kube_ray_list["items"]:
            print(
                "{:<63s}{:>2s}".format(
                    cluster["metadata"]["name"],
                    cluster["metadata"]["namespace"],
                )
            )
    print(line)

    if "items" in kube_ray_list:
        for cluster in kube_ray_list["items"]:
            print("deleting raycluster = {}".format(cluster["metadata"]["name"]))
            my_kuberay_api.delete_ray_cluster(
                name=cluster["metadata"]["name"],
                k8s_namespace=cluster["metadata"]["namespace"],
            )


if __name__ == "__main__":
    main()

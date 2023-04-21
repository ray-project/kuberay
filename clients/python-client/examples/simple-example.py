from python_client import kuberay_cluster_api
from python_client.utils import kuberay_cluster_utils, kuberay_cluster_builder

def main():
    print("starting cluster handler...")
    cluster_name = "new-cluster0"
    cluster_namespace = "default"
    my_kuberay_api = kuberay_cluster_api.RayClusterApi() # this is the main api object
    my_cluster_director = kuberay_cluster_builder.Director()
    my_cluster_util = kuberay_cluster_utils.ClusterUtils()
    cluster0 = my_cluster_director.build_small_cluster(name="new-cluster0")

    if cluster0:
        my_kuberay_api.create_ray_cluster(body=cluster0)
        is_running = my_kuberay_api.wait_until_ray_cluster_running(
            name=cluster_name,
            k8s_namespace=cluster_namespace,
        )
        if not is_running:
            print("cluster do not start well")
        output, success = my_cluster_util.exec_file("script/test.py", cluster_name, cluster_namespace)
        if success:
            print(output)
        else:
            print("Error in exec python file")
        my_kuberay_api.delete_ray_cluster(cluster_name, cluster_namespace)
    else:
        print("Error in build small cluster")
    

if __name__ == "__main__":
    main()

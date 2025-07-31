import unittest
from python_client import kuberay_job_api, kuberay_cluster_api, constants
from python_client.utils import kuberay_cluster_builder


class TestUtils(unittest.TestCase):
    def __init__(self, methodName: str = ...) -> None:
        super().__init__(methodName)

        self.api = kuberay_job_api.RayjobApi()
        self.cluster_api = kuberay_cluster_api.RayClusterApi()
        self.director = kuberay_cluster_builder.Director()

    def test_submit_ray_job_to_existing_cluster(self):
        """Test submitting a job to an existing cluster using clusterSelector."""
        # Create a cluster using the director
        cluster_name = "premade"
        namespace = "default"

        # Build a small cluster
        cluster_body = self.director.build_small_cluster(
            name=cluster_name,
            k8s_namespace=namespace,
            labels={"ray.io/cluster": cluster_name},
        )

        # Ensure cluster was built successfully
        self.assertIsNotNone(cluster_body, "Cluster should be built successfully")
        self.assertEqual(cluster_body["metadata"]["name"], cluster_name)

        # Create the cluster in Kubernetes
        created_cluster = self.cluster_api.create_ray_cluster(
            body=cluster_body, k8s_namespace=namespace
        )

        self.assertIsNotNone(created_cluster, "Cluster should be created successfully")

        self.cluster_api.wait_until_ray_cluster_running(cluster_name, namespace, 60, 10)
        job_name = "premade-cluster-job"
        try:
            # Create job spec with clusterSelector
            job_body = {
                "apiVersion": constants.GROUP + "/" + constants.JOB_VERSION,
                "kind": constants.JOB_KIND,
                "metadata": {
                    "name": job_name,
                    "namespace": namespace,
                    "labels": {
                        "app.kubernetes.io/name": job_name,
                        "app.kubernetes.io/managed-by": "kuberay",
                    },
                },
                "spec": {
                    "clusterSelector": {
                        "ray.io/cluster": cluster_name,
                    },
                    "entrypoint": 'python -c "import time; time.sleep(20)"',
                    "submissionMode": "K8sJobMode",
                },
            }

            submitted_job = self.api.submit_job(
                job=job_body,
                k8s_namespace=namespace,
            )

            self.assertIsNotNone(submitted_job, "Job should be submitted successfully")
            self.assertEqual(submitted_job["metadata"]["name"], job_name)
            self.assertEqual(
                submitted_job["spec"]["clusterSelector"]["ray.io/cluster"], cluster_name
            )

            self.api.wait_until_job_finished(job_name, namespace, 120, 10)
        finally:
            self.cluster_api.delete_ray_cluster(
                name=cluster_name, k8s_namespace=namespace
            )

            self.api.delete_job(job_name, namespace)

    def test_get_job_status(self):
        """Test getting job status for a running job."""
        # Create a cluster using the director
        cluster_name = "status-test-cluster"
        namespace = "default"

        # Build a small cluster
        cluster_body = self.director.build_small_cluster(
            name=cluster_name,
            k8s_namespace=namespace,
            labels={"ray.io/cluster": cluster_name},
        )

        # Create the cluster in Kubernetes
        created_cluster = self.cluster_api.create_ray_cluster(
            body=cluster_body, k8s_namespace=namespace
        )
        self.assertIsNotNone(created_cluster, "Cluster should be created successfully")

        # Wait for cluster to be running
        self.cluster_api.wait_until_ray_cluster_running(cluster_name, namespace, 60, 10)

        job_name = "status-test-job"
        try:
            # Create job spec with clusterSelector
            job_body = {
                "apiVersion": constants.GROUP + "/" + constants.JOB_VERSION,
                "kind": constants.JOB_KIND,
                "metadata": {
                    "name": job_name,
                    "namespace": namespace,
                    "labels": {
                        "app.kubernetes.io/name": job_name,
                        "app.kubernetes.io/managed-by": "kuberay",
                    },
                },
                "spec": {
                    "clusterSelector": {
                        "ray.io/cluster": cluster_name,
                    },
                    "entrypoint": 'python -c "import time; time.sleep(30)"',
                    "submissionMode": "K8sJobMode",
                },
            }

            # Submit the job
            submitted_job = self.api.submit_job(
                job=job_body,
                k8s_namespace=namespace,
            )
            self.assertIsNotNone(submitted_job, "Job should be submitted successfully")

            # Test getting job status - should return status after some time
            status = self.api.get_job_status(
                job_name, namespace, timeout=30, delay_between_attempts=2
            )
            self.assertIsNotNone(status, "Job status should be retrieved")

            # Check for expected fields in the status (based on actual output)
            self.assertIn(
                "jobDeploymentStatus",
                status,
                "Status should contain jobDeploymentStatus field",
            )
            self.assertIn("jobId", status, "Status should contain jobId field")
            self.assertIn(
                "rayClusterName", status, "Status should contain rayClusterName field"
            )

            # Wait for job to finish
            self.api.wait_until_job_finished(job_name, namespace, 60, 5)

            # Test getting final status
            final_status = self.api.get_job_status(
                job_name, namespace, timeout=10, delay_between_attempts=1
            )
            self.assertIsNotNone(final_status, "Final job status should be retrieved")

            # Check that the job completed successfully (based on the logs showing SUCCEEDED)
            # The actual jobStatus might be in rayJobInfo or a different field
            self.assertIn(
                "jobDeploymentStatus",
                final_status,
                "Final status should contain jobDeploymentStatus field",
            )

        finally:
            # Clean up
            self.cluster_api.delete_ray_cluster(
                name=cluster_name, k8s_namespace=namespace
            )
            self.api.delete_job(job_name, namespace)

    def test_wait_until_job_finished(self):
        """Test waiting for job completion."""
        # Create a cluster using the director
        cluster_name = "wait-test-cluster"
        namespace = "default"

        # Build a small cluster
        cluster_body = self.director.build_small_cluster(
            name=cluster_name,
            k8s_namespace=namespace,
            labels={"ray.io/cluster": cluster_name},
        )

        # Create the cluster in Kubernetes
        created_cluster = self.cluster_api.create_ray_cluster(
            body=cluster_body, k8s_namespace=namespace
        )
        self.assertIsNotNone(created_cluster, "Cluster should be created successfully")

        # Wait for cluster to be running
        self.cluster_api.wait_until_ray_cluster_running(
            cluster_name, namespace, 180, 10
        )

        job_name = "wait-test-job"
        try:
            # Create job spec with clusterSelector - short running job
            job_body = {
                "apiVersion": constants.GROUP + "/" + constants.JOB_VERSION,
                "kind": constants.JOB_KIND,
                "metadata": {
                    "name": job_name,
                    "namespace": namespace,
                    "labels": {
                        "app.kubernetes.io/name": job_name,
                        "app.kubernetes.io/managed-by": "kuberay",
                    },
                },
                "spec": {
                    "clusterSelector": {
                        "ray.io/cluster": cluster_name,
                    },
                    "entrypoint": "python -c \"print('Hello from Ray job'); import time; time.sleep(5)\"",
                    "submissionMode": "K8sJobMode",
                },
            }

            # Submit the job
            submitted_job = self.api.submit_job(
                job=job_body,
                k8s_namespace=namespace,
            )
            self.assertIsNotNone(submitted_job, "Job should be submitted successfully")

            # Test waiting for job completion
            result = self.api.wait_until_job_finished(
                job_name, namespace, timeout=180, delay_between_attempts=2
            )
            self.assertTrue(result, "Job should complete successfully within timeout")

        finally:
            # Clean up
            self.cluster_api.delete_ray_cluster(
                name=cluster_name, k8s_namespace=namespace
            )
            self.api.delete_job(job_name, namespace)

    def test_delete_job(self):
        """Test deleting a job."""
        # Create a cluster using the director
        cluster_name = "delete-test-cluster"
        namespace = "default"

        # Build a small cluster
        cluster_body = self.director.build_small_cluster(
            name=cluster_name,
            k8s_namespace=namespace,
            labels={"ray.io/cluster": cluster_name},
        )

        # Create the cluster in Kubernetes
        created_cluster = self.cluster_api.create_ray_cluster(
            body=cluster_body, k8s_namespace=namespace
        )
        self.assertIsNotNone(created_cluster, "Cluster should be created successfully")

        # Wait for cluster to be running
        self.cluster_api.wait_until_ray_cluster_running(cluster_name, namespace, 60, 10)

        job_name = "delete-test-job"
        try:
            # Create job spec with clusterSelector
            job_body = {
                "apiVersion": constants.GROUP + "/" + constants.JOB_VERSION,
                "kind": constants.JOB_KIND,
                "metadata": {
                    "name": job_name,
                    "namespace": namespace,
                    "labels": {
                        "app.kubernetes.io/name": job_name,
                        "app.kubernetes.io/managed-by": "kuberay",
                    },
                },
                "spec": {
                    "clusterSelector": {
                        "ray.io/cluster": cluster_name,
                    },
                    "entrypoint": "python -c \"print('Job to be deleted'); import time; time.sleep(10)\"",
                    "submissionMode": "K8sJobMode",
                },
            }

            # Submit the job
            submitted_job = self.api.submit_job(
                job=job_body,
                k8s_namespace=namespace,
            )
            self.assertIsNotNone(submitted_job, "Job should be submitted successfully")

            # Wait for job to finish
            self.api.wait_until_job_finished(job_name, namespace, 60, 5)

            # Test deleting the job
            delete_result = self.api.delete_job(job_name, namespace)
            self.assertTrue(delete_result, "Job should be deleted successfully")

        finally:
            # Clean up cluster
            self.cluster_api.delete_ray_cluster(
                name=cluster_name, k8s_namespace=namespace
            )

    def test_get_job_status_nonexistent_job(self):
        """Test getting status for a non-existent job."""
        # Test getting status for a job that doesn't exist
        status = self.api.get_job_status(
            "nonexistent-job", "default", timeout=2, delay_between_attempts=2
        )
        self.assertIsNone(status, "Status should be None for non-existent job")

    def test_wait_until_job_finished_nonexistent_job(self):
        """Test waiting for completion of a non-existent job."""
        # Test waiting for a job that doesn't exist
        result = self.api.wait_until_job_finished(
            "nonexistent-job", "default", timeout=2, delay_between_attempts=2
        )
        self.assertFalse(result, "Should return False for non-existent job")

    def test_delete_job_nonexistent_job(self):
        """Test deleting a non-existent job."""
        # Test deleting a job that doesn't exist
        result = self.api.delete_job("nonexistent-job", "default")
        self.assertFalse(result, "Should return False for non-existent job")

    def test_submit_job_invalid_spec(self):
        """Test submitting a job with invalid specification."""
        # Test submitting a job with invalid spec
        invalid_job = {
            "apiVersion": "invalid/version",
            "kind": "InvalidKind",
            "metadata": {
                "name": "invalid-job",
                "namespace": "default",
            },
            "spec": {
                "invalidField": "invalidValue",
            },
        }

        result = self.api.submit_job(job=invalid_job, k8s_namespace="default")
        self.assertIsNone(result, "Should return None for invalid job specification")

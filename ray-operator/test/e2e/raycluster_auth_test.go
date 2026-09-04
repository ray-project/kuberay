package e2e

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// NewRayClusterSpecWithAuth creates a new RayClusterSpec with the specified AuthMode.
func NewRayClusterSpecWithAuth(authMode rayv1.AuthMode) *rayv1ac.RayClusterSpecApplyConfiguration {
	return NewRayClusterSpec().
		WithAuthOptions(rayv1ac.AuthOptions().WithMode(authMode))
}

func TestRayClusterAuthOptions(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	test.T().Run("RayCluster with token authentication enabled", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-auth-token", namespace.Name).
			WithSpec(NewRayClusterSpecWithAuth(rayv1.AuthModeToken).WithRayVersion("2.52"))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully with AuthModeToken", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(headPod).NotTo(BeNil())

		// Verify Ray container has auth token env vars
		VerifyContainerAuthTokenEnvVars(test, rayCluster, &headPod.Spec.Containers[utils.RayContainerIndex])

		// Verify worker pods have auth token env vars
		workerPods, err := GetWorkerPods(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(workerPods).ToNot(BeEmpty())
		for _, workerPod := range workerPods {
			VerifyContainerAuthTokenEnvVars(test, rayCluster, &workerPod.Spec.Containers[utils.RayContainerIndex])
		}

		// Get auth token for job submission tests
		authToken := getAuthTokenFromPod(test, rayCluster, headPod)
		g.Expect(authToken).NotTo(BeEmpty(), "Auth token should be present")

		// Test job submission with auth token using Ray Job CLI
		test.T().Run("Submit job with auth token should succeed", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH auth token")

			submissionId := fmt.Sprintf("test-job-with-auth-%d", time.Now().Unix())

			// Submit job via Ray Job CLI with auth token
			// Set RAY_AUTH_TOKEN environment variable for authentication
			submitCmd := []string{
				"bash", "-c",
				fmt.Sprintf("RAY_AUTH_TOKEN=%s ray job submit --address http://127.0.0.1:8265 --submission-id %s --no-wait -- python -c 'import ray; ray.init(); print(\"Job with auth succeeded\")'",
					authToken, submissionId),
			}

			stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, submitCmd)

			// Verify job was submitted successfully
			g.Expect(stdout.String()).To(ContainSubstring(submissionId), "Job submission should succeed with valid auth token")

			// Verify job status is queryable with auth token (confirms auth works)
			g.Eventually(func(g Gomega) {
				statusCmd := []string{
					"bash", "-c",
					fmt.Sprintf("RAY_AUTH_TOKEN=%s ray job status --address http://127.0.0.1:8265 %s", authToken, submissionId),
				}
				stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, statusCmd)
				g.Expect(stdout.String()).To(ContainSubstring("succeeded"))
			}, TestTimeoutShort).Should(Succeed())

			LogWithTimestamp(test.T(), "Successfully submitted and verified job with auth token")
		})

		test.T().Run("Submit job with incorrect auth token should fail", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH incorrect auth token (should fail)")

			submissionId := fmt.Sprintf("test-job-bad-auth-%d", time.Now().Unix())

			// Submit job via Ray Job CLI with INCORRECT auth token
			incorrectToken := "incorrect-token-12345"
			submitCmd := []string{
				"bash", "-c",
				fmt.Sprintf("RAY_AUTH_TOKEN=%s ray job submit --address http://127.0.0.1:8265 --submission-id %s --no-wait -- python -c 'print(\"Should not run\")'",
					incorrectToken, submissionId),
			}

			_, stderr := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, submitCmd, true)

			// Verify response indicates authentication failure
			g.Expect(stderr.String()).To(ContainSubstring("Unauthenticated"), "Job submission should fail with Unauthorized when auth token is incorrect")

			LogWithTimestamp(test.T(), "Job submission correctly rejected with incorrect auth token")
		})
	})

	test.T().Run("RayCluster with authOptions.enableK8sTokenAuth=true", func(t *testing.T) {
		t.Parallel()

		cleanup := setupAuthRBAC(test, namespace.Name)
		defer cleanup()

		rayClusterAC := rayv1ac.RayCluster("raycluster-auth-k8s-token", namespace.Name).
			WithSpec(NewRayClusterSpecWithAuth(rayv1.AuthModeToken).
				WithRayVersion("2.55").
				WithAuthOptions(rayv1ac.AuthOptions().
					WithMode(rayv1.AuthModeToken).
					WithEnableK8sTokenAuth(true)))

		// Set the ServiceAccountName to "raylet" as we bound the permissions to this SA
		rayClusterAC.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = ptr.To("raylet")
		for i := range rayClusterAC.Spec.WorkerGroupSpecs {
			rayClusterAC.Spec.WorkerGroupSpecs[i].Template.Spec.ServiceAccountName = ptr.To("raylet")
		}

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully with AuthModeToken and EnableK8sTokenAuth", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(headPod).NotTo(BeNil())

		VerifyContainerEnvVar(test, &headPod.Spec.Containers[utils.RayContainerIndex], utils.RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR, "true")

		VerifyContainerVolumeMount(test, &headPod.Spec.Containers[utils.RayContainerIndex], utils.RayTokenVolumeName, utils.RayTokenMountPath)

		test.T().Run("Submit job with K8s auth token should succeed", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH K8s auth token")
			submissionId := fmt.Sprintf("test-job-k8s-auth-%d", time.Now().Unix())

			tokenPath := fmt.Sprintf("%s/token", utils.RayTokenMountPath)
			submitCmd := []string{
				"bash", "-c",
				fmt.Sprintf("export RAY_AUTH_TOKEN=$(cat %s) && ray job submit --address http://127.0.0.1:8265 --submission-id %s --no-wait -- python -c 'import ray; ray.init(); print(\"Job with K8s auth succeeded\")'",
					tokenPath, submissionId),
			}

			stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, submitCmd)
			g.Expect(stdout.String()).To(ContainSubstring(submissionId), "Job submission should succeed with valid K8s auth token")
			LogWithTimestamp(test.T(), "Successfully submitted job with K8s auth token")
		})
	})

	test.T().Run("RayCluster with authOptions.secretName", func(t *testing.T) {
		t.Parallel()

		secretName := "test-token-secret" // #nosec G101
		token := "my-custom-secret-token-123"

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace.Name,
			},
			StringData: map[string]string{
				utils.RAY_AUTH_TOKEN_SECRET_KEY: token,
			},
		}
		_, err := test.Client().Core().CoreV1().Secrets(namespace.Name).Create(test.Ctx(), secret, metav1.CreateOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created Secret %s/%s successfully", namespace.Name, secretName)

		rayClusterAC := rayv1ac.RayCluster("raycluster-auth-secret", namespace.Name).
			WithSpec(NewRayClusterSpecWithAuth(rayv1.AuthModeToken).
				WithRayVersion("2.52").
				WithAuthOptions(rayv1ac.AuthOptions().
					WithMode(rayv1.AuthModeToken).
					WithSecretName(secretName)))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully with AuthModeToken and SecretName", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())

		VerifyContainerEnvVarFromSecret(test, &headPod.Spec.Containers[utils.RayContainerIndex], utils.RAY_AUTH_TOKEN_ENV_VAR, secretName, utils.RAY_AUTH_TOKEN_SECRET_KEY)

		test.T().Run("Submit job with custom auth token should succeed", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH custom auth token")
			submissionId := fmt.Sprintf("test-job-custom-auth-%d", time.Now().Unix())

			submitCmd := []string{
				"bash", "-c",
				fmt.Sprintf("RAY_AUTH_TOKEN=%s ray job submit --address http://127.0.0.1:8265 --submission-id %s --no-wait -- python -c 'import ray; ray.init(); print(\"Job with custom auth succeeded\")'",
					token, submissionId),
			}

			stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, submitCmd)
			g.Expect(stdout.String()).To(ContainSubstring(submissionId), "Job submission should succeed with valid custom auth token")
			LogWithTimestamp(test.T(), "Successfully submitted job with custom auth token")
		})
	})
}

// getAuthTokenFromPod extracts the auth token from the pod's environment variables.
// It reads the token from the secret referenced by the RAY_AUTH_TOKEN environment variable.
func getAuthTokenFromPod(test Test, rayCluster *rayv1.RayCluster, pod *corev1.Pod) string {
	test.T().Helper()
	g := NewWithT(test.T())

	for _, envVar := range pod.Spec.Containers[utils.RayContainerIndex].Env {
		if envVar.Name == utils.RAY_AUTH_TOKEN_ENV_VAR {
			if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
				secret, err := test.Client().Core().CoreV1().Secrets(rayCluster.Namespace).
					Get(test.Ctx(), envVar.ValueFrom.SecretKeyRef.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				return string(secret.Data[envVar.ValueFrom.SecretKeyRef.Key])
			}
		}
	}
	return ""
}

func VerifyContainerEnvVar(test Test, container *corev1.Container, envName, envValue string) {
	test.T().Helper()
	g := NewWithT(test.T())

	var envVar *corev1.EnvVar
	for _, env := range container.Env {
		if env.Name == envName {
			envVar = &env
			break
		}
	}
	g.Expect(envVar).NotTo(BeNil(), "Environment variable %s should be set in container %s", envName, container.Name)
	g.Expect(envVar.Value).To(Equal(envValue), "Environment variable %s should have value %s in container %s", envName, envValue, container.Name)
}

func VerifyContainerVolumeMount(test Test, container *corev1.Container, mountName, mountPath string) {
	test.T().Helper()
	g := NewWithT(test.T())

	var mount *corev1.VolumeMount
	for _, m := range container.VolumeMounts {
		if m.Name == mountName {
			mount = &m
			break
		}
	}
	g.Expect(mount).NotTo(BeNil(), "Volume mount %s should be present in container %s", mountName, container.Name)
	g.Expect(mount.MountPath).To(Equal(mountPath), "Volume mount %s should have path %s in container %s", mountName, mountPath, container.Name)
}

func VerifyContainerEnvVarFromSecret(test Test, container *corev1.Container, envName, secretName, secretKey string) {
	test.T().Helper()
	g := NewWithT(test.T())

	var envVar *corev1.EnvVar
	for _, env := range container.Env {
		if env.Name == envName {
			envVar = &env
			break
		}
	}
	g.Expect(envVar).NotTo(BeNil(), "Environment variable %s should be set in container %s", envName, container.Name)
	g.Expect(envVar.ValueFrom).NotTo(BeNil(), "Environment variable %s should be populated from a source in container %s", envName, container.Name)
	g.Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Environment variable %s should be populated from a secret in container %s", envName, container.Name)
	g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secretName), "Secret name should be %s in container %s", secretName, container.Name)
	g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal(secretKey), "Secret key should be %s in container %s", secretKey, container.Name)
}

func setupAuthRBAC(test Test, namespace string) func() {
	test.T().Helper()
	g := NewWithT(test.T())
	ctx := test.Ctx()

	// 1. Create ServiceAccount "raylet"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raylet",
			Namespace: namespace,
		},
	}
	_, err := test.Client().Core().CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create ServiceAccount raylet")

	// 2. Create ClusterRole "ray-authenticator"
	crAuthenticatorName := fmt.Sprintf("ray-authenticator-%s", namespace)
	crAuthenticator := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: crAuthenticatorName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{"authorization.k8s.io"},
				Resources: []string{"subjectaccessreviews"},
				Verbs:     []string{"create"},
			},
		},
	}
	_, err = test.Client().Core().RbacV1().ClusterRoles().Create(ctx, crAuthenticator, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRole %s", crAuthenticatorName)

	// 3. Create ClusterRole "ray-writer"
	crWriterName := fmt.Sprintf("ray-writer-%s", namespace)
	crWriter := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: crWriterName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"ray.io"},
				Resources: []string{"rayclusters"},
				Verbs:     []string{"ray:write"},
			},
		},
	}
	_, err = test.Client().Core().RbacV1().ClusterRoles().Create(ctx, crWriter, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRole %s", crWriterName)

	// 4. Create ClusterRoleBinding "ray-authenticator-<namespace>"
	crbAuthenticatorName := fmt.Sprintf("ray-authenticator-%s", namespace)
	crbAuthenticator := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: crbAuthenticatorName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "raylet",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     crAuthenticatorName,
		},
	}
	_, err = test.Client().Core().RbacV1().ClusterRoleBindings().Create(ctx, crbAuthenticator, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding %s", crbAuthenticatorName)

	// 5. Create RoleBinding "raylet" in namespace bound to "ray-writer" ClusterRole
	rbWriterName := "raylet"
	rbWriter := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbWriterName,
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "raylet",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     crWriterName,
		},
	}
	_, err = test.Client().Core().RbacV1().RoleBindings(namespace).Create(ctx, rbWriter, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create RoleBinding %s", rbWriterName)

	return func() {
		// Cleanup ClusterRoles and ClusterRoleBindings (Namespace resources are cleaned up by test framework usually, but explicit is fine for CRs)
		_ = test.Client().Core().RbacV1().ClusterRoles().Delete(ctx, crAuthenticatorName, metav1.DeleteOptions{})
		_ = test.Client().Core().RbacV1().ClusterRoles().Delete(ctx, crWriterName, metav1.DeleteOptions{})
		_ = test.Client().Core().RbacV1().ClusterRoleBindings().Delete(ctx, crbAuthenticatorName, metav1.DeleteOptions{})
	}
}

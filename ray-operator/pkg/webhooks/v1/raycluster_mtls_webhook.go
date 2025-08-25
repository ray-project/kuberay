package v1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// RayClusterMTLSWebhook handles MTLS injection for Ray clusters
type RayClusterMTLSWebhook struct {
	Config *configapi.Configuration
}

var _ webhook.CustomDefaulter = &RayClusterMTLSWebhook{}

// Default implements webhook.Defaulter
func (w *RayClusterMTLSWebhook) Default(ctx context.Context, obj runtime.Object) error {
	if !ptr.Deref(w.Config.EnableMTLS, false) {
		return nil
	}

	rayCluster := obj.(*rayv1.RayCluster)
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Injecting MTLS configuration", "cluster", rayCluster.Name)

	// Inject MTLS configuration into head group
	w.injectMTLSIntoHeadGroup(rayCluster)

	// Inject MTLS configuration into worker groups
	w.injectMTLSIntoWorkerGroups(rayCluster)

	return nil
}

// injectMTLSIntoHeadGroup injects MTLS configuration into the head group
func (w *RayClusterMTLSWebhook) injectMTLSIntoHeadGroup(rayCluster *rayv1.RayCluster) {
	// Add environment variables for Ray TLS
	w.addTLSEnvironmentVariables(&rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0])

	// Add init container for certificate generation
	w.addCertInitContainer(&rayCluster.Spec.HeadGroupSpec.Template.Spec, true)

	// Add CA volumes
	w.addCAVolumes(&rayCluster.Spec.HeadGroupSpec.Template.Spec)

	// Add certificate volume mounts
	w.addCertVolumeMounts(&rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0])
}

// injectMTLSIntoWorkerGroups injects MTLS configuration into all worker groups
func (w *RayClusterMTLSWebhook) injectMTLSIntoWorkerGroups(rayCluster *rayv1.RayCluster) {
	for i := range rayCluster.Spec.WorkerGroupSpecs {
		workerSpec := &rayCluster.Spec.WorkerGroupSpecs[i]

		// Add environment variables for Ray TLS
		w.addTLSEnvironmentVariables(&workerSpec.Template.Spec.Containers[0])

		// Add init container for certificate generation
		w.addCertInitContainer(&workerSpec.Template.Spec, false)

		// Add CA volumes
		w.addCAVolumes(&workerSpec.Template.Spec)

		// Add certificate volume mounts
		w.addCertVolumeMounts(&workerSpec.Template.Spec.Containers[0])
	}
}

// addTLSEnvironmentVariables adds Ray TLS environment variables to a container
func (w *RayClusterMTLSWebhook) addTLSEnvironmentVariables(container *corev1.Container) {
	tlsEnvVars := []corev1.EnvVar{
		{
			Name: "MY_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{Name: "RAY_USE_TLS", Value: "1"},
		{Name: "RAY_TLS_SERVER_CERT", Value: "/home/ray/workspace/tls/server.crt"},
		{Name: "RAY_TLS_SERVER_KEY", Value: "/home/ray/workspace/tls/server.key"},
		{Name: "RAY_TLS_CA_CERT", Value: "/home/ray/workspace/tls/ca.crt"},
	}

	container.Env = append(container.Env, tlsEnvVars...)
}

// addCertInitContainer adds an init container for certificate generation
func (w *RayClusterMTLSWebhook) addCertInitContainer(podSpec *corev1.PodSpec, isHead bool) {
	initContainer := corev1.Container{
		Name:         "create-cert",
		Image:        w.Config.CertGeneratorImage,
		Command:      []string{"sh", "-c", w.generateCertCommand(isHead)},
		VolumeMounts: w.getCertVolumeMounts(),
	}

	podSpec.InitContainers = append(podSpec.InitContainers, initContainer)
}

// generateCertCommand generates the OpenSSL command for certificate creation
func (w *RayClusterMTLSWebhook) generateCertCommand(isHead bool) string {
	if isHead {
		return `cd /home/ray/workspace/tls &&
				openssl req -nodes -newkey rsa:2048 -keyout server.key -out server.csr -subj '/CN=ray-head' &&
				printf "authorityKeyIdentifier=keyid,issuer\nbasicConstraints=CA:FALSE\nsubjectAltName = @alt_names\n[alt_names]\nDNS.1 = 127.0.0.1\nDNS.2 = localhost\nDNS.3 = ${FQ_RAY_IP}\nDNS.4 = $(awk 'END{print $1}' /etc/hosts)">./domain.ext &&
				cp /home/ray/workspace/ca/* . &&
				openssl x509 -req -CA ca.crt -CAkey ca.key -in server.csr -out server.crt -days 365 -CAcreateserial -extfile domain.ext`
	}

	return `cd /home/ray/workspace/tls &&
			openssl req -nodes -newkey rsa:2048 -keyout server.key -out server.csr -subj '/CN=ray-worker' &&
			printf "authorityKeyIdentifier=keyid,issuer\nbasicConstraints=CA:FALSE\nsubjectAltName = @alt_names\n[alt_names]\nDNS.1 = 127.0.0.1\nDNS.2 = localhost\nDNS.3 = ${FQ_RAY_IP}\nDNS.4 = $(awk 'END{print $1}' /etc/hosts)">./domain.ext &&
			cp /home/ray/workspace/ca/* . &&
			openssl x509 -req -CA ca.crt -CAkey ca.key -in server.csr -out server.crt -days 365 -CAcreateserial -extfile domain.ext`
}

// addCAVolumes adds CA and certificate volumes to a pod spec
func (w *RayClusterMTLSWebhook) addCAVolumes(podSpec *corev1.PodSpec) {
	caVolumes := []corev1.Volume{
		{
			Name: "ca-vol",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: w.getCASecretName(),
				},
			},
		},
		{
			Name: "server-cert",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	podSpec.Volumes = append(podSpec.Volumes, caVolumes...)
}

// addCertVolumeMounts adds certificate volume mounts to a container
func (w *RayClusterMTLSWebhook) addCertVolumeMounts(container *corev1.Container) {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "ca-vol",
			MountPath: "/home/ray/workspace/ca",
			ReadOnly:  true,
		},
		{
			Name:      "server-cert",
			MountPath: "/home/ray/workspace/tls",
			ReadOnly:  false,
		},
	}

	container.VolumeMounts = append(container.VolumeMounts, volumeMounts...)
}

// getCertVolumeMounts returns the volume mounts needed for the init container
func (w *RayClusterMTLSWebhook) getCertVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "ca-vol",
			MountPath: "/home/ray/workspace/ca",
			ReadOnly:  true,
		},
		{
			Name:      "server-cert",
			MountPath: "/home/ray/workspace/tls",
			ReadOnly:  false,
		},
	}
}

// getCASecretName returns the name of the CA secret
func (w *RayClusterMTLSWebhook) getCASecretName() string {
	// This will be managed by the controller
	return "ray-ca-secret"
}

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestResourceNamer(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	namer := NewResourceNamer(cluster)

	t.Run("ServiceAccountName for OAuth", func(t *testing.T) {
		name := namer.ServiceAccountName(ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-proxy-sa", name)
	})

	t.Run("ServiceAccountName for OIDC", func(t *testing.T) {
		name := namer.ServiceAccountName(ModeOIDC)
		assert.Equal(t, "test-cluster-oidc-proxy-sa", name)
	})

	t.Run("SecretName for OAuth", func(t *testing.T) {
		name := namer.SecretName(ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-config", name)
	})

	t.Run("SecretName for OIDC", func(t *testing.T) {
		name := namer.SecretName(ModeOIDC)
		assert.Equal(t, "test-cluster-oidc-config", name)
	})

	t.Run("TLSSecretName for OAuth", func(t *testing.T) {
		name := namer.TLSSecretName(ModeIntegratedOAuth)
		assert.Equal(t, "test-cluster-oauth-tls", name)
	})

	t.Run("TLSSecretName for OIDC", func(t *testing.T) {
		name := namer.TLSSecretName(ModeOIDC)
		assert.Equal(t, "test-cluster-oidc-tls", name)
	})

	t.Run("ConfigMapName", func(t *testing.T) {
		name := namer.ConfigMapName()
		assert.Equal(t, "kube-rbac-proxy-config-test-cluster", name)
	})
}

func TestInjectAuthSidecar(t *testing.T) {
	cluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
	}

	mockGetSidecar := func(*rayv1.RayCluster) corev1.Container {
		return corev1.Container{
			Name:  "test-sidecar",
			Image: "test-image:latest",
		}
	}

	mockGetVolumes := func(*rayv1.RayCluster) []corev1.Volume {
		return []corev1.Volume{
			{
				Name: "test-volume",
			},
		}
	}

	t.Run("OAuth sidecar injection when enabled", func(t *testing.T) {
		podSpec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		}

		result := InjectAuthSidecar(
			podSpec,
			cluster,
			ModeIntegratedOAuth,
			true, // controlledNetworkEnv enabled
			mockGetSidecar,
			mockGetVolumes,
			"test-service-account",
		)

		assert.True(t, result.Injected)
		assert.Equal(t, ModeIntegratedOAuth, result.AuthType)
		assert.Equal(t, 2, result.ContainerCount) // main + sidecar
		assert.Equal(t, "test-service-account", result.ServiceAccountName)
		assert.Len(t, podSpec.Containers, 2)
		assert.Len(t, podSpec.Volumes, 1)
		assert.Equal(t, "test-sidecar", podSpec.Containers[1].Name)
	})

	t.Run("OAuth sidecar not injected when disabled", func(t *testing.T) {
		podSpec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		}

		result := InjectAuthSidecar(
			podSpec,
			cluster,
			ModeIntegratedOAuth,
			false, // controlledNetworkEnv disabled
			mockGetSidecar,
			mockGetVolumes,
			"test-service-account",
		)

		assert.False(t, result.Injected)
		assert.Len(t, podSpec.Containers, 1) // Only main container
		assert.Empty(t, podSpec.Volumes)
	})

	t.Run("OIDC sidecar injection when enabled", func(t *testing.T) {
		podSpec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		}

		result := InjectAuthSidecar(
			podSpec,
			cluster,
			ModeOIDC,
			true, // controlledNetworkEnv enabled
			mockGetSidecar,
			mockGetVolumes,
			"", // No service account for OIDC
		)

		assert.True(t, result.Injected)
		assert.Equal(t, ModeOIDC, result.AuthType)
		assert.Equal(t, 2, result.ContainerCount)
		assert.Equal(t, "", result.ServiceAccountName)
		assert.Len(t, podSpec.Containers, 2)
	})

	t.Run("Wrong auth mode doesn't inject", func(t *testing.T) {
		podSpec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		}

		// Try to inject OAuth but auth mode is OIDC
		result := InjectAuthSidecar(
			podSpec,
			cluster,
			ModeOIDC,
			true,
			mockGetSidecar,
			mockGetVolumes,
			"test-service-account",
		)

		// Should inject OIDC, not OAuth
		assert.True(t, result.Injected)
		assert.Equal(t, ModeOIDC, result.AuthType)
	})
}

func TestCreateProbe(t *testing.T) {
	config := ProbeConfig{
		Path:                "/healthz",
		Port:                8443,
		Scheme:              corev1.URISchemeHTTPS,
		InitialDelaySeconds: 10,
		TimeoutSeconds:      5,
		PeriodSeconds:       15,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	probe := CreateProbe(config)

	assert.NotNil(t, probe)
	assert.NotNil(t, probe.HTTPGet)
	assert.Equal(t, "/healthz", probe.HTTPGet.Path)
	assert.Equal(t, int32(8443), probe.HTTPGet.Port.IntVal)
	assert.Equal(t, corev1.URISchemeHTTPS, probe.HTTPGet.Scheme)
	assert.Equal(t, int32(10), probe.InitialDelaySeconds)
	assert.Equal(t, int32(5), probe.TimeoutSeconds)
	assert.Equal(t, int32(15), probe.PeriodSeconds)
	assert.Equal(t, int32(1), probe.SuccessThreshold)
	assert.Equal(t, int32(3), probe.FailureThreshold)
}

func TestStandardProxyResources(t *testing.T) {
	resources := StandardProxyResources()

	assert.NotNil(t, resources.Requests)
	assert.NotNil(t, resources.Limits)

	// Check requests
	cpuRequest := resources.Requests[corev1.ResourceCPU]
	memRequest := resources.Requests[corev1.ResourceMemory]
	assert.Equal(t, "10m", cpuRequest.String())
	assert.Equal(t, "20Mi", memRequest.String())

	// Check limits
	cpuLimit := resources.Limits[corev1.ResourceCPU]
	memLimit := resources.Limits[corev1.ResourceMemory]
	assert.Equal(t, "200m", cpuLimit.String())
	assert.Equal(t, "100Mi", memLimit.String())
}

func TestCreateSecretVolume(t *testing.T) {
	volume := CreateSecretVolume("my-volume", "my-secret")

	assert.Equal(t, "my-volume", volume.Name)
	assert.NotNil(t, volume.VolumeSource.Secret)
	assert.Equal(t, "my-secret", volume.VolumeSource.Secret.SecretName)
}

func TestCreateConfigMapVolume(t *testing.T) {
	volume := CreateConfigMapVolume("my-volume", "my-configmap")

	assert.Equal(t, "my-volume", volume.Name)
	assert.NotNil(t, volume.VolumeSource.ConfigMap)
	assert.Equal(t, "my-configmap", volume.VolumeSource.ConfigMap.Name)
}

func TestCreateVolumeMount(t *testing.T) {
	mount := CreateVolumeMount("my-volume", "/etc/config", true)

	assert.Equal(t, "my-volume", mount.Name)
	assert.Equal(t, "/etc/config", mount.MountPath)
	assert.True(t, mount.ReadOnly)
}

func TestCreateContainerPort(t *testing.T) {
	port := CreateContainerPort(8443, "https")

	assert.Equal(t, int32(8443), port.ContainerPort)
	assert.Equal(t, "https", port.Name)
	assert.Equal(t, corev1.ProtocolTCP, port.Protocol)
}

func TestCreateEnvVarFromSecret(t *testing.T) {
	envVar := CreateEnvVarFromSecret("MY_SECRET", "secret-name", "secret-key")

	assert.Equal(t, "MY_SECRET", envVar.Name)
	assert.NotNil(t, envVar.ValueFrom)
	assert.NotNil(t, envVar.ValueFrom.SecretKeyRef)
	assert.Equal(t, "secret-name", envVar.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "secret-key", envVar.ValueFrom.SecretKeyRef.Key)
}

func TestFormatOAuthDelegateURLs(t *testing.T) {
	result := FormatOAuthDelegateURLs("test-namespace")

	expected := `{"/":{"resource":"pods","namespace":"test-namespace","verb":"get"}}`
	assert.Equal(t, expected, result)
}

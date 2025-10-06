package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// AuthProxyConfig holds configuration for authentication proxy sidecars
type AuthProxyConfig struct {
	GetSidecar     func(*rayv1.RayCluster) corev1.Container
	GetVolumes     func(*rayv1.RayCluster) []corev1.Volume
	ContainerName  string
	Image          string
	PortName       string
	ServiceAccount string
	AuthType       AuthenticationMode
	Port           int32
}

// SidecarInjectionResult holds the result of sidecar injection
type SidecarInjectionResult struct {
	AuthType           AuthenticationMode
	ServiceAccountName string
	ContainerCount     int
	Injected           bool
}

// InjectAuthSidecar injects the appropriate authentication sidecar based on the authentication mode
// This reduces duplication between OAuth and OIDC injection logic
func InjectAuthSidecar(
	podSpec *corev1.PodSpec,
	cluster *rayv1.RayCluster,
	authMode AuthenticationMode,
	controlledNetworkEnv bool,
	getSidecarFunc func(*rayv1.RayCluster) corev1.Container,
	getVolumesFunc func(*rayv1.RayCluster) []corev1.Volume,
	serviceAccountName string,
) SidecarInjectionResult {
	result := SidecarInjectionResult{
		Injected: false,
		AuthType: authMode,
	}

	// Determine if sidecar should be injected based on auth mode
	shouldInject := false
	switch authMode {
	case ModeIntegratedOAuth:
		shouldInject = ShouldEnableOAuth(controlledNetworkEnv, authMode)
	case ModeOIDC:
		shouldInject = ShouldEnableOIDC(controlledNetworkEnv, authMode)
	}

	if !shouldInject {
		return result
	}

	// Inject sidecar and volumes
	sidecar := getSidecarFunc(cluster)
	volumes := getVolumesFunc(cluster)

	podSpec.Containers = append(podSpec.Containers, sidecar)
	podSpec.Volumes = append(podSpec.Volumes, volumes...)

	// Set service account if provided
	if serviceAccountName != "" {
		podSpec.ServiceAccountName = serviceAccountName
	}

	result.Injected = true
	result.ContainerCount = len(podSpec.Containers)
	result.ServiceAccountName = podSpec.ServiceAccountName

	return result
}

// ResourceNamer provides a consistent interface for naming authentication-related resources
type ResourceNamer struct {
	Cluster *rayv1.RayCluster
}

// NewResourceNamer creates a new ResourceNamer for the given cluster
func NewResourceNamer(cluster *rayv1.RayCluster) *ResourceNamer {
	return &ResourceNamer{Cluster: cluster}
}

// ServiceAccountName returns the OAuth/OIDC service account name
func (r *ResourceNamer) ServiceAccountName(authType AuthenticationMode) string {
	switch authType {
	case ModeIntegratedOAuth:
		return r.Cluster.Name + "-oauth-proxy-sa"
	case ModeOIDC:
		return r.Cluster.Name + "-oidc-proxy-sa"
	default:
		return r.Cluster.Name + "-proxy-sa"
	}
}

// SecretName returns the OAuth/OIDC secret name
func (r *ResourceNamer) SecretName(authType AuthenticationMode) string {
	switch authType {
	case ModeIntegratedOAuth:
		return r.Cluster.Name + "-oauth-config"
	case ModeOIDC:
		return r.Cluster.Name + "-oidc-config"
	default:
		return r.Cluster.Name + "-auth-config"
	}
}

// TLSSecretName returns the OAuth/OIDC TLS secret name
func (r *ResourceNamer) TLSSecretName(authType AuthenticationMode) string {
	switch authType {
	case ModeIntegratedOAuth:
		return r.Cluster.Name + "-oauth-tls"
	case ModeOIDC:
		return r.Cluster.Name + "-oidc-tls"
	default:
		return r.Cluster.Name + "-auth-tls"
	}
}

// ConfigMapName returns the config map name for OIDC
func (r *ResourceNamer) ConfigMapName() string {
	return "kube-rbac-proxy-config-" + r.Cluster.Name
}

// ProbeConfig holds common probe configuration
type ProbeConfig struct {
	Path                string
	Scheme              corev1.URIScheme
	Port                int32
	InitialDelaySeconds int32
	TimeoutSeconds      int32
	PeriodSeconds       int32
	SuccessThreshold    int32
	FailureThreshold    int32
}

// CreateProbe creates a standardized HTTP probe
func CreateProbe(config ProbeConfig) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   config.Path,
				Port:   intstr.FromInt(int(config.Port)),
				Scheme: config.Scheme,
			},
		},
		InitialDelaySeconds: config.InitialDelaySeconds,
		TimeoutSeconds:      config.TimeoutSeconds,
		PeriodSeconds:       config.PeriodSeconds,
		SuccessThreshold:    config.SuccessThreshold,
		FailureThreshold:    config.FailureThreshold,
	}
}

// StandardProxyResources returns standard resource limits for proxy sidecars
func StandardProxyResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("20Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
}

// CreateSecretVolume creates a volume from a secret
func CreateSecretVolume(volumeName, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

// CreateConfigMapVolume creates a volume from a config map
func CreateConfigMapVolume(volumeName, configMapName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}
}

// CreateVolumeMount creates a volume mount
func CreateVolumeMount(name, mountPath string, readOnly bool) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      name,
		MountPath: mountPath,
		ReadOnly:  readOnly,
	}
}

// CreateContainerPort creates a container port
func CreateContainerPort(port int32, name string) corev1.ContainerPort {
	return corev1.ContainerPort{
		ContainerPort: port,
		Name:          name,
		Protocol:      corev1.ProtocolTCP,
	}
}

// CreateEnvVarFromSecret creates an environment variable from a secret
func CreateEnvVarFromSecret(envName, secretName, secretKey string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
				Key: secretKey,
			},
		},
	}
}

// FormatOAuthDelegateURLs formats the OpenShift OAuth delegate URLs
func FormatOAuthDelegateURLs(namespace string) string {
	return fmt.Sprintf(`{"/":{"resource":"pods","namespace":"%s","verb":"get"}}`, namespace)
}

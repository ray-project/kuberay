package common

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	historyServerCollectorContainerName = "collector"
	historyServerCollectorDefaultImage  = "kuberay/collector:latest"
	historyServerDefaultRuntimeClass    = "s3"
	// Events port for the collector HTTP server. Port 8080 is taken by Ray's
	// metrics exporter, so we use 8084 (matching the existing manual manifests).
	historyServerEventsPort = "8084"

	// Event export address injected into the Ray container. The port must
	// match historyServerEventsPort above.
	historyServerEventsExportAddr = "http://localhost:" + historyServerEventsPort + "/v1/events"

	// nodeIDExtractionScript extracts the raylet node ID from /proc and writes
	// it to the shared volume. It uses awk instead of Perl-regex grep (grep -oP)
	// for portability across base images (Alpine, busybox, ubi-minimal, etc.).
	nodeIDExtractionScript = `
while true; do
  node_id=$(ps -eo args 2>/dev/null | awk '/raylet\/raylet.*--node_id=/{
    for(i=1;i<=NF;i++){
      if(match($i,/^--node_id=/)){
        print substr($i,RSTART+10)
      }
    }
  }')
  if [ -n "$node_id" ]; then
    echo "$node_id" > /tmp/ray/raylet_node_id
    break
  fi
  sleep 1
done`
)

// InjectHistoryServerCollector injects the History Server Collector sidecar,
// shared volume, postStart lifecycle hook, and Ray event environment variables
// into the given pod. It is a no-op if HistoryServerCollector is not configured
// on the RayCluster spec.
//
// This follows the same pattern as autoscaler sidecar injection in BuildPod().
func InjectHistoryServerCollector(ctx context.Context, opts *rayv1.HistoryServerCollectorOptions, pod *corev1.Pod, rayNodeType rayv1.RayNodeType, clusterName, clusterNamespace string) {
	log := ctrl.LoggerFrom(ctx)

	if opts == nil {
		return
	}
	log.Info("Injecting History Server Collector sidecar")

	rayContainer := &pod.Spec.Containers[utils.RayContainerIndex]

	// 1. Ensure a shared /tmp/ray volume exists on the Ray container.
	//    Reuse RayLogVolumeName so we share the same volume the autoscaler
	//    injection in BuildPod() uses. addEmptyDir is a no-op when a volume
	//    is already mounted at /tmp/ray, which is what we want.
	addEmptyDir(ctx, rayContainer, pod, RayLogVolumeName, RayLogVolumeMountPath, corev1.StorageMediumDefault)

	// Look up the actual volume name mounted at /tmp/ray on the Ray container.
	// It will usually be RayLogVolumeName, but a user-supplied pod template
	// could already have a different volume mounted there — in that case we
	// need to reuse its name so the collector sidecar mounts the same volume.
	sharedVolumeName := volumeNameAtMountPath(rayContainer, RayLogVolumeMountPath)
	if sharedVolumeName == "" {
		log.Info("Unable to resolve shared volume name for History Server Collector; skipping injection",
			"expectedMountPath", RayLogVolumeMountPath)
		return
	}

	// 2. Inject postStart lifecycle hook for node ID extraction.
	//    Preserve any existing postStart hook by only injecting when it's unset.
	if rayContainer.Lifecycle == nil {
		rayContainer.Lifecycle = &corev1.Lifecycle{}
	}
	if rayContainer.Lifecycle.PostStart == nil {
		rayContainer.Lifecycle.PostStart = &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", nodeIDExtractionScript},
			},
		}
	} else {
		log.Info("Ray container already has a postStart hook; skipping node ID extraction injection")
	}

	// 3. Inject Ray event export environment variables.
	injectEnvIfMissing(rayContainer, "RAY_enable_ray_event", "true")
	injectEnvIfMissing(rayContainer, "RAY_enable_core_worker_ray_event_to_aggregator", "true")
	injectEnvIfMissing(rayContainer, "RAY_DASHBOARD_AGGREGATOR_AGENT_EVENTS_EXPORT_ADDR", historyServerEventsExportAddr)

	// 4. Build and append the Collector sidecar container.
	role := "Worker"
	if rayNodeType == rayv1.HeadNode {
		role = "Head"
	}
	pod.Spec.Containers = append(pod.Spec.Containers, buildCollectorContainer(opts, sharedVolumeName, role, clusterName, clusterNamespace))
}

// volumeNameAtMountPath returns the name of the volume mounted at the given
// path on the container, or an empty string if no such mount exists.
func volumeNameAtMountPath(container *corev1.Container, mountPath string) string {
	for _, m := range container.VolumeMounts {
		if m.MountPath == mountPath {
			return m.Name
		}
	}
	return ""
}

func buildCollectorContainer(opts *rayv1.HistoryServerCollectorOptions, sharedVolumeName, role, clusterName, clusterNamespace string) corev1.Container {
	image := historyServerCollectorDefaultImage
	if opts.Image != nil && *opts.Image != "" {
		image = *opts.Image
	}

	pullPolicy := corev1.PullIfNotPresent
	if opts.ImagePullPolicy != nil {
		pullPolicy = *opts.ImagePullPolicy
	}

	runtimeClass := historyServerDefaultRuntimeClass
	if opts.RuntimeClassName != nil && *opts.RuntimeClassName != "" {
		runtimeClass = *opts.RuntimeClassName
	}

	resources := defaultCollectorResources()
	if opts.Resources != nil {
		resources = *opts.Resources
	}

	return corev1.Container{
		Name:            historyServerCollectorContainerName,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command: []string{"collector"},
		Args: []string{
			"--role=" + role,
			"--runtime-class-name=" + runtimeClass,
			"--ray-cluster-name=" + clusterName,
			"--ray-cluster-namespace=" + clusterNamespace,
			"--ray-root-dir=" + RayLogVolumeMountPath,
			"--events-port=" + historyServerEventsPort,
		},
		Env:     opts.Env,
		EnvFrom: opts.EnvFrom,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      sharedVolumeName,
				MountPath: RayLogVolumeMountPath,
				ReadOnly:  false,
			},
		},
		Resources: resources,
	}
}

func defaultCollectorResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

func injectEnvIfMissing(container *corev1.Container, name, value string) {
	if !utils.EnvVarExists(name, container.Env) {
		container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
	}
}

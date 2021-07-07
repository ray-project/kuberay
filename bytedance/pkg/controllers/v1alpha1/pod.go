package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	rayiov1alpha1 "github.com/ray-project/ray-contrib/bytedance/pkg/api/v1alpha1"
	"github.com/ray-project/ray-contrib/bytedance/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *RayClusterReconciler) createHeadPod(cluster rayiov1alpha1.RayCluster) error {
	pod := r.buildHeadPod(cluster)

	r.Log.Info("createHeadPod", "head pod with name", pod.GenerateName)
	if err := r.Create(context.TODO(), pod); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					r.Log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", pod.Name)
					return err
				}
			}
			r.Log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			return err
		}
	}
	r.Recorder.Eventf(&cluster, corev1.EventTypeNormal, "Created", "Created head pod %s", pod.Name)
	return nil
}

func (r *RayClusterReconciler) createWorkerPod(cluster rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerNodeSpec) error {
	// build the pod then create it
	headSvcName := fmt.Sprintf("%s-%s", cluster.Name, "head")
	pod := r.buildWorkerPod(cluster, worker, headSvcName)
	replica := pod
	if err := r.Create(context.TODO(), replica); err != nil {
		if errors.IsAlreadyExists(err) {
			fetchedPod := corev1.Pod{}
			// the pod might be in terminating state, we need to check
			if errPod := r.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &fetchedPod); errPod == nil {
				if fetchedPod.DeletionTimestamp != nil {
					r.Log.Error(errPod, "create pod error!", "pod is in a terminating state, we will wait until it is cleaned up", pod.Name)
					return err
				}
			}
			r.Log.Info("Creating pod", "Pod already exists", pod.Name)
		} else {
			r.Log.Error(fmt.Errorf("createWorkerPod error"), "error creating pod", "pod", pod)
			return err
		}
	}
	r.Log.Info("Created pod", "Pod ", pod.Name)
	r.Recorder.Eventf(&cluster, corev1.EventTypeNormal, "Created", "Created worker pod %s", pod.Name)
	return nil
}

// Build head instance pod(s).
func (r *RayClusterReconciler) buildHeadPod(cluster rayiov1alpha1.RayCluster) *corev1.Pod {
	podType := rayiov1alpha1.HeadNode
	podName := strings.ToLower(cluster.Name + "-" + string(podType) + "-")
	// TODO: use one util to generate the name
	headServiceName := fmt.Sprintf("%s-%s", cluster.Name, "head")
	pod := buildPodTemplate(cluster, cluster.Spec.HeadNodeSpec.Template, podType,
		cluster.Spec.HeadNodeSpec.Params, podName, headServiceName, "")
	// Set raycluster cluster as the owner and controller
	if err := controllerutil.SetControllerReference(&cluster, pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// Build worker instance pods.
func (r *RayClusterReconciler) buildWorkerPod(cluster rayiov1alpha1.RayCluster, worker rayiov1alpha1.WorkerNodeSpec, svcName string) *corev1.Pod {
	podType := rayiov1alpha1.WorkerNode
	podName := strings.ToLower(cluster.Name + "-" + string(podType) + "-" + worker.NodeGroupName + "-")
	//podConf := DefaultWorkerPodConfig(cluster, worker, podType, podName, svcName)

	pod := buildPodTemplate(cluster, worker.Template, podType, worker.Params, podName, svcName, worker.NodeGroupName)
	// Set raycluster cluster as the owner and controller
	if err := controllerutil.SetControllerReference(&cluster, pod, r.Scheme); err != nil {
		r.Log.Error(err, "Failed to set controller reference for raycluster pod")
	}

	return pod
}

// buildPodTemplate builds a pod template.
func buildPodTemplate(cluster rayiov1alpha1.RayCluster, template corev1.PodTemplateSpec, rayNodeType rayiov1alpha1.RayNodeType, params map[string]string, podName, svcName, groupName string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: template.ObjectMeta,
		Spec:       template.Spec,
	}

	// override the settings.
	pod.GenerateName = podName
	pod.Namespace = cluster.Namespace
	pod.ObjectMeta.Labels = utils.GeneratePodLabels(string(rayNodeType), cluster.Name, groupName, template.Labels)

	// Set container environments
	// Set container environments
	index := utils.FindRayContainerIndex(pod.Spec)
	for index := range pod.Spec.InitContainers {
		utils.SetInitContainerEnvs(&pod.Spec.InitContainers[index], svcName)
	}
	utils.SetContainerEnvs(&pod.Spec.Containers[index], rayNodeType, params, svcName)

	// TODO (Jeffwan@): should we use the upstream way? let user make the decision.
	// The down side is user need to take care too many arguments, env and ip setting would be tricky
	params = utils.AddServiceAddress(params, rayNodeType, svcName)
	rayParamPairs := utils.BuildCommandFromParams(rayNodeType, params)
	pod.Spec.Containers[index].Command = []string{"/bin/bash", "-c", "--"}
	// Let's use daemon mode and deprecate `sleep infinity` because it's hard to track logs
	pod.Spec.Containers[index].Args = []string{fmt.Sprintf("%s && %s", rayParamPairs, "sleep infinity")}

	return pod
}

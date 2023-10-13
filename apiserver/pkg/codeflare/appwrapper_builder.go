package codeflare

import (
	"fmt"

	mcadApi "github.com/project-codeflare/multi-cluster-app-dispatcher/pkg/apis/controller/v1beta1"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type AppWrapperConverter interface {
	AppWrapperFromRayCluster(rayCluster *rayv1api.RayCluster) (*mcadApi.AppWrapper, error)
	RayClusterFromAppWrapper(clusterAppWrapper *mcadApi.AppWrapper) (*rayv1api.RayCluster, error)
	AppWrapperFromRayJob(rayJob *rayv1api.RayJob) (*mcadApi.AppWrapper, error)
	RayJobFromAppWrapper(jobAppWrapper *mcadApi.AppWrapper) (*rayv1api.RayJob, error)
}

type defaultAppwrapperConverter struct {
	AppWrapperConverter
	scheme *runtime.Scheme
}

func NewAppWrapperConverter(scheme *runtime.Scheme) AppWrapperConverter {
	return &defaultAppwrapperConverter{scheme: scheme}
}

func (awb *defaultAppwrapperConverter) AppWrapperFromRayCluster(rayCluster *rayv1api.RayCluster) (*mcadApi.AppWrapper, error) {
	output, err := awb.rayClusterToYAML(rayCluster)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize cluster to yaml: %w", err)
	}
	requests, limits := awb.calculateClusterAggregateResources(&rayCluster.Spec.HeadGroupSpec, rayCluster.Spec.WorkerGroupSpecs)
	clusterAppWrapper := awb.makeAppWrapper(rayCluster.Name, rayCluster.Namespace, requests, limits, output)
	return clusterAppWrapper, nil
}

func (awb *defaultAppwrapperConverter) RayClusterFromAppWrapper(clusterAppWrapper *mcadApi.AppWrapper) (*rayv1api.RayCluster, error) {
	bytez := clusterAppWrapper.Spec.AggrResources.GenericItems[0].GenericTemplate.Raw
	codec := serializer.NewCodecFactory(awb.scheme, serializer.EnablePretty).LegacyCodec(rayv1api.SchemeGroupVersion)
	rayCluster := &rayv1api.RayCluster{}
	if err := runtime.DecodeInto(codec, bytez, rayCluster); err != nil {
		return nil, err
	}
	return rayCluster, nil
}

func (awb *defaultAppwrapperConverter) AppWrapperFromRayJob(rayJob *rayv1api.RayJob) (*mcadApi.AppWrapper, error) {
	output, err := awb.rayJobToYAML(rayJob)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize cluster to yaml: %w", err)
	}
	requests, limits := awb.calculateJobAggregateResources(rayJob)
	jobAppWrapper := awb.makeAppWrapper(rayJob.Name, rayJob.Namespace, requests, limits, output)
	return jobAppWrapper, nil
}

func (awb *defaultAppwrapperConverter) RayJobFromAppWrapper(jobAppWrapper *mcadApi.AppWrapper) (*rayv1api.RayJob, error) {
	bytez := jobAppWrapper.Spec.AggrResources.GenericItems[0].GenericTemplate.Raw
	codec := serializer.NewCodecFactory(awb.scheme, serializer.EnablePretty).LegacyCodec(rayv1api.SchemeGroupVersion)
	rayJob := &rayv1api.RayJob{}
	if err := runtime.DecodeInto(codec, bytez, rayJob); err != nil {
		return nil, err
	}
	return rayJob, nil
}

func (awb *defaultAppwrapperConverter) calculateClusterAggregateResources(clusterHeadSpec *rayv1api.HeadGroupSpec, workerSpecs []rayv1api.WorkerGroupSpec) (corev1.ResourceList, corev1.ResourceList) {
	var replicaCount int
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	awb.addContainersResources(clusterHeadSpec.Template.Spec.Containers, requests, limits)
	awb.addContainersResources(clusterHeadSpec.Template.Spec.InitContainers, requests, limits)

	for _, workerGroupSpec := range workerSpecs {
		replicaCount += int(*workerGroupSpec.Replicas)
		// calculate the resources for all the replica counts.
		for i := 0; i < replicaCount; i++ {
			// for each container in the worker group accumulate all the resources and the limits
			awb.addContainersResources(workerGroupSpec.Template.Spec.Containers, requests, limits)
			awb.addContainersResources(workerGroupSpec.Template.Spec.InitContainers, requests, limits)
		}
	}
	return requests, limits
}

func (awb *defaultAppwrapperConverter) calculateJobAggregateResources(rayJob *rayv1api.RayJob) (corev1.ResourceList, corev1.ResourceList) {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	if rayJob.Spec.RayClusterSpec != nil {
		requests, limits = awb.calculateClusterAggregateResources(&rayJob.Spec.RayClusterSpec.HeadGroupSpec, rayJob.Spec.RayClusterSpec.WorkerGroupSpecs)
	}
	if rayJob.Spec.SubmitterPodTemplate == nil {
		// Add default resources for the submitter pod
		awb.addResourceLists(requests, corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		})
		awb.addResourceLists(limits, corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		})
	} else {
		awb.addContainersResources(rayJob.Spec.SubmitterPodTemplate.Spec.Containers, requests, limits)
		awb.addContainersResources(rayJob.Spec.SubmitterPodTemplate.Spec.InitContainers, requests, limits)
	}
	return requests, limits
}

func (awb *defaultAppwrapperConverter) rayClusterToYAML(rayCluster *rayv1api.RayCluster) ([]byte, error) {
	codec := serializer.NewCodecFactory(awb.scheme, serializer.EnablePretty).LegacyCodec(rayv1api.SchemeGroupVersion)
	return runtime.Encode(codec, rayCluster)
}

func (awb *defaultAppwrapperConverter) rayJobToYAML(rayJob *rayv1api.RayJob) ([]byte, error) {
	codec := serializer.NewCodecFactory(awb.scheme, serializer.EnablePretty).LegacyCodec(rayv1api.SchemeGroupVersion)
	return runtime.Encode(codec, rayJob)
}

func (awb *defaultAppwrapperConverter) addContainersResources(containers []corev1.Container, requests corev1.ResourceList, limits corev1.ResourceList) {
	for _, container := range containers {
		awb.addResourceLists(requests, container.Resources.Requests)
		awb.addResourceLists(limits, container.Resources.Limits)
	}
}

func (awb *defaultAppwrapperConverter) addResourceLists(accumulator, increment corev1.ResourceList) {
	for name, quantity := range increment {
		if accumulatedQuantity, ok := accumulator[name]; ok {
			accumulatedQuantity.Add(quantity)
			accumulator[name] = accumulatedQuantity
		} else {
			accumulator[name] = quantity
		}
	}
}

func (awb *defaultAppwrapperConverter) makeAppWrapper(name, namespace string, requests corev1.ResourceList, limits corev1.ResourceList, crdBytes []byte) *mcadApi.AppWrapper {
	clusterAppWrapper := &mcadApi.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				util.KubernetesManagedByLabelKey: util.ComponentName,
			},
		},
		Spec: mcadApi.AppWrapperSpec{
			AggrResources: mcadApi.AppWrapperResourceList{
				GenericItems: []mcadApi.AppWrapperGenericResource{
					{
						CustomPodResources: []mcadApi.CustomPodResourceTemplate{
							{
								Replicas: 1,
								Requests: requests,
								Limits:   limits,
							},
						},
						GenericTemplate: runtime.RawExtension{
							Raw: crdBytes,
						},
					},
				},
			},
		},
	}
	return clusterAppWrapper
}

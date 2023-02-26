package v1beta1

import (
	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	"strconv"
)

// ConvertTo converts this CronJob to the Hub version (v1alpha1).
func (src *RayCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha1.RayCluster)

	// from annotation to Spec
	if src.ObjectMeta.Annotations == nil {
		if value, exist := src.ObjectMeta.Annotations["ray.io/enable-autoscaling"]; exist {
			if flag, err := strconv.ParseBool(value); err == nil {
				dst.Spec.EnableInTreeAutoscaling = &flag
			}
		}

		if value, exist := src.ObjectMeta.Annotations["ray.io/enable-ingress"]; exist {
			if flag, err := strconv.ParseBool(value); err == nil {
				dst.Spec.HeadGroupSpec.EnableIngress = &flag
			}
		}

		if value, exist := src.ObjectMeta.Annotations["ray.io/version"]; exist {
			dst.Spec.RayVersion = value
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version.
func (dst *RayCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha1.RayCluster)

	if dst.ObjectMeta.Annotations == nil {
		dst.ObjectMeta.Annotations = make(map[string]string)
	}

	if src.Spec.EnableInTreeAutoscaling != nil {
		dst.ObjectMeta.Annotations["ray.io/enable-autoscaling"] = strconv.FormatBool(*src.Spec.EnableInTreeAutoscaling)
	}

	if src.Spec.HeadGroupSpec.EnableIngress != nil {
		dst.ObjectMeta.Annotations["ray.io/enable-autoscaling"] = strconv.FormatBool(*src.Spec.HeadGroupSpec.EnableIngress)
	}

	dst.ObjectMeta.Annotations["ray.io/version"] = src.Spec.RayVersion

	return nil
}

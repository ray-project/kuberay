package v1beta1

import (
	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta1) to the Hub version (v1alpha1).
func (src *RayService) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*rayv1alpha1.RayService)
	if err := Convert_v1beta1_RayService_To_v1alpha1_RayService(src, dst, nil); err != nil {
		return err
	}
	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version (v1beta1).
func (dst *RayService) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*rayv1alpha1.RayService)
	if err := Convert_v1alpha1_RayService_To_v1beta1_RayService(src, dst, nil); err != nil {
		return err
	}
	return nil
}

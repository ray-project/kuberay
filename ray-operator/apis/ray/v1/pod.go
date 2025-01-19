package v1

import "strings"

func IsGCSFaultToleranceEnabled(instance RayCluster) bool {
	v, ok := instance.Annotations[RayFTEnabledAnnotationKey]
	return (ok && strings.ToLower(v) == "true") || instance.Spec.GcsFaultToleranceOptions != nil
}

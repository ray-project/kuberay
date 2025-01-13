package v1

import corev1 "k8s.io/api/core/v1"

func EnvVarExists(envName string, envVars []corev1.EnvVar) bool {
	for _, env := range envVars {
		if env.Name == envName {
			return true
		}
	}
	return false
}

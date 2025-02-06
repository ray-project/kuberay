package support

import (
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestNamespace(t Test, options ...Option[*corev1.Namespace]) *corev1.Namespace {
	t.T().Helper()
	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-ns-",
		},
	}

	for _, option := range options {
		require.NoError(t.T(), option.applyTo(namespace))
	}

	namespace, err := t.Client().Core().CoreV1().Namespaces().Create(t.Ctx(), namespace, metav1.CreateOptions{})
	require.NoError(t.T(), err)

	return namespace
}

func deleteTestNamespace(t Test, namespace *corev1.Namespace) {
	t.T().Helper()
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Core().CoreV1().Namespaces().Delete(t.Ctx(), namespace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	require.NoError(t.T(), err)
}

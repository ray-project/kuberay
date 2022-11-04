package common

import (
	"bytes"
	"reflect"
	"testing"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func longString(t *testing.T) string {
	var b bytes.Buffer
	for i := 0; i < 200; i++ {
		b.WriteString("a")
	}
	result := b.String()
	// Confirm length.
	assert.Equal(t, len(result), 200)
	return result
}

func shortString(t *testing.T) string {
	result := utils.CheckName(longString(t))
	// Confirm length.
	assert.Equal(t, len(result), 50)
	return result
}

// Test subject and role ref names in the function BuildRoleBinding.
func TestBuildRoleBindingSubjectAndRoleRefName(t *testing.T) {
	tests := map[string]struct {
		input *rayiov1alpha1.RayCluster
		want  []string
	}{
		"Ray cluster with head group service account": {
			input: &rayiov1alpha1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayiov1alpha1.RayClusterSpec{
					HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								ServiceAccountName: "my-service-account",
							},
						},
					},
				},
			},
			want: []string{"my-service-account", "raycluster-sample"},
		},
		"Ray cluster without head group service account": {
			input: &rayiov1alpha1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayiov1alpha1.RayClusterSpec{
					HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{},
						},
					},
				},
			},
			want: []string{"raycluster-sample", "raycluster-sample"},
		},
		"Ray cluster with a very long name and without head group service account": {
			input: &rayiov1alpha1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      longString(t),
					Namespace: "default",
				},
				Spec: rayiov1alpha1.RayClusterSpec{
					HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{},
						},
					},
				},
			},
			want: []string{shortString(t), shortString(t)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rb, err := BuildRoleBinding(tc.input)
			assert.Nil(t, err)
			got := []string{rb.Subjects[0].Name, rb.RoleRef.Name}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

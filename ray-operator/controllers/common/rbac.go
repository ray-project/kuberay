package common

import (
	"github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildServiceAccount creates a new ServiceAccount for a head pod with autoscaler.
func BuildServiceAccount(cluster *v1alpha1.RayCluster) (*v1.ServiceAccount, error) {
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				RayClusterLabelKey: cluster.Name,
			},
		},
	}

	return sa, nil
}

// BuildRole creates a new Role for an RayCluster resource.
func BuildRole(cluster *v1alpha1.RayCluster) (*rbacv1.Role, error) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				RayClusterLabelKey: cluster.Name,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"ray.io"},
				Resources: []string{"rayclusters"},
				Verbs:     []string{"get", "patch"},
			},
		},
	}

	return role, nil
}

// BuildRole
func BuildRoleBinding(cluster *v1alpha1.RayCluster) (*rbacv1.RoleBinding, error) {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				RayClusterLabelKey: cluster.Name,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     cluster.Name,
		},
	}

	return rb, nil
}

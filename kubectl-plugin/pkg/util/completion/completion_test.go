package completion

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayClientFake "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
)

func TestRayResourceTypeCompletionFunc(t *testing.T) {
	compFunc := RayResourceTypeCompletionFunc()
	comps, directive := compFunc(nil, []string{}, "")
	checkCompletion(t, comps, []string{"raycluster", "rayjob", "rayservice"}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func checkCompletion(t *testing.T, comps, expectedComps []string, directive, expectedDirective cobra.ShellCompDirective) {
	if e, d := expectedDirective, directive; e != d {
		t.Errorf("expected directive\n%v\nbut got\n%v", e, d)
	}

	sort.Strings(comps)
	sort.Strings(expectedComps)

	require.ElementsMatch(t, expectedComps, comps)
}

func TestWorkerGroupCompletionFunc(t *testing.T) {
	tests := []struct {
		name              string
		namespace         string
		clusterName       string
		allNamespaces     bool
		args              []string
		toComplete        string
		rayClusters       []runtime.Object
		expectedComps     []string
		expectedDirective cobra.ShellCompDirective
	}{
		{
			name:          "should return workergroup names in default namespace when no flag is set",
			namespace:     "",
			allNamespaces: false,
			clusterName:   "",
			args:          []string{},
			toComplete:    "",
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: "group-1",
							},
							{
								GroupName: "group-4",
							},
						},
					},
				},
			},
			expectedComps:     []string{"group-1", "group-4"},
			expectedDirective: cobra.ShellCompDirectiveNoFileComp,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kubeClientSet := kubefake.NewClientset()
			rayClient := rayClientFake.NewSimpleClientset(tc.rayClusters...)
			k8sClient := client.NewClientForTesting(kubeClientSet, rayClient)

			cmd := &cobra.Command{}
			cmd.Flags().String("namespace", tc.namespace, "")
			cmd.Flags().String("ray-cluster", tc.clusterName, "")
			cmd.Flags().Bool("all-namespaces", tc.allNamespaces, "")

			comps, directive := workerGroupCompletionFunc(cmd, tc.args, tc.toComplete, k8sClient)
			checkCompletion(t, comps, tc.expectedComps, directive, cobra.ShellCompDirectiveNoFileComp)
		})
	}
}

func TestNodeCompletionFunc(t *testing.T) {
	tests := []struct {
		name              string
		namespace         string
		allNamespaces     bool
		clusterName       string
		args              []string
		toComplete        string
		pods              []runtime.Object
		expectedComps     []string
		expectedDirective cobra.ShellCompDirective
	}{
		{
			name:          "should return node names in default namespace when no flag is set",
			namespace:     "",
			allNamespaces: false,
			clusterName:   "",
			args:          []string{},
			toComplete:    "",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
						Name:      "pod-1",
						Labels: map[string]string{
							util.RayClusterLabelKey:   "cluster-1",
							util.RayIsRayNodeLabelKey: "yes",
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
						Name:      "pod-2",
						Labels: map[string]string{
							util.RayClusterLabelKey:   "cluster-1",
							util.RayIsRayNodeLabelKey: "yes",
						},
					},
				},
			},
			expectedComps:     []string{"pod-1", "pod-2"},
			expectedDirective: cobra.ShellCompDirectiveNoFileComp,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kubeClientSet := kubefake.NewClientset(tc.pods...)
			rayClient := rayClientFake.NewSimpleClientset()
			k8sClient := client.NewClientForTesting(kubeClientSet, rayClient)

			cmd := &cobra.Command{}
			cmd.Flags().String("namespace", tc.namespace, "")
			cmd.Flags().String("ray-cluster", tc.clusterName, "")
			cmd.Flags().Bool("all-namespaces", tc.allNamespaces, "")

			comps, directive := nodeCompletionFunc(cmd, tc.args, tc.toComplete, k8sClient)
			checkCompletion(t, comps, tc.expectedComps, directive, cobra.ShellCompDirectiveNoFileComp)
		})
	}
}

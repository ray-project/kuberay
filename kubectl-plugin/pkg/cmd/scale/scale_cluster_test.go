package scale

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	kubefake "k8s.io/client-go/kubernetes/fake"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayClientFake "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
)

func TestRayScaleClusterComplete(t *testing.T) {
	tests := []struct {
		name              string
		namespace         string
		expectedNamespace string
		args              []string
	}{
		{
			name:              "namespace should be set to 'default' if not specified",
			args:              []string{"my-cluster"},
			expectedNamespace: "default",
		},
		{
			name:              "namespace and cluster should be set correctly",
			args:              []string{"my-cluster"},
			namespace:         "DEADBEEF",
			expectedNamespace: "DEADBEEF",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
			cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

			fakeScaleClusterOptions := NewScaleClusterOptions(cmdFactory, testStreams)

			cmd := &cobra.Command{}
			cmd.Flags().StringVarP(&fakeScaleClusterOptions.namespace, "namespace", "n", tc.namespace, "")

			err := fakeScaleClusterOptions.Complete(tc.args, cmd)

			require.NoError(t, err)
			assert.Equal(t, tc.expectedNamespace, fakeScaleClusterOptions.namespace)
			assert.Equal(t, tc.args[0], fakeScaleClusterOptions.cluster)
		})
	}
}

func TestRayScaleClusterValidate(t *testing.T) {
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	tests := []struct {
		name        string
		opts        *ScaleClusterOptions
		expect      string
		expectError string
	}{
		{
			name: "should error when no worker group is set",
			opts: &ScaleClusterOptions{
				cmdFactory: cmdFactory,
			},
			expectError: "must specify -w/--worker-group",
		},
		{
			name: "should error when no parameters are set",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
			},
			expectError: "must specify at least one of --replicas, --min-replicas, or --max-replicas (non-negative integers)",
		},
		{
			name: "should error when replicas is negative",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				replicas:    ptr.To(int32(-2)),
			},
			expectError: "--replicas must be a non-negative integer",
		},
		{
			name: "should error when min-replicas is negative",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				minReplicas: ptr.To(int32(-2)),
			},
			expectError: "--min-replicas must be a non-negative integer",
		},
		{
			name: "should error when max-replicas is negative",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				maxReplicas: ptr.To(int32(-2)),
			},
			expectError: "--max-replicas must be a non-negative integer",
		},
		{
			name: "should error when min-replicas is greater than max_replicas",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				minReplicas: ptr.To(int32(5)),
				maxReplicas: ptr.To(int32(3)),
			},
			expectError: fmt.Sprintf("--min-replicas (%d) cannot be greater than --max-replicas (%d)", 5, 3),
		},
		{
			name: "should error when replicas is less than min_replicas",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				minReplicas: ptr.To(int32(3)),
				replicas:    ptr.To(int32(2)),
			},
			expectError: fmt.Sprintf("--replicas (%d) cannot be less than --min-replicas (%d)", 2, 3),
		},
		{
			name: "should error when replicas is greater than max_replicas",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				maxReplicas: ptr.To(int32(5)),
				replicas:    ptr.To(int32(7)),
			},
			expectError: fmt.Sprintf("--replicas (%d) cannot be greater than --max-replicas (%d)", 7, 5),
		},
		{
			name: "successful validation call",
			opts: &ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				workerGroup: "test-worker-group",
				replicas:    ptr.To(int32(4)),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRayScaleClusterRun(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	testNamespace, workerGroup, cluster := "test-context", "worker-group-1", "my-cluster"
	desiredReplicas := int32(7)

	tests := []struct {
		name           string
		expectedOutput string
		expectedError  string
		rayClusters    []runtime.Object
		replicas       int32
	}{
		{
			name:          "should error when cluster doesn't exist",
			rayClusters:   []runtime.Object{},
			expectedError: "failed to scale worker group",
		},
		{
			name: "should error when worker group doesn't exist",
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
					},
				},
			},
			expectedError: fmt.Sprintf("worker group %s not found", workerGroup),
		},
		{
			name:     "should not do anything when the desired replicas is the same as the current replicas",
			replicas: desiredReplicas,
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: workerGroup,
								Replicas:  &desiredReplicas,
							},
						},
					},
				},
			},
			expectedOutput: fmt.Sprintf("Worker group %s in Ray cluster %s in namespace %s already matches the requested configuration. Skipping.\n",
				workerGroup, cluster, testNamespace),
		},
		{
			name:     "should succeed when arguments are valid",
			replicas: desiredReplicas,
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: workerGroup,
								Replicas:  ptr.To(int32(1)),
							},
						},
					},
				},
			},
			expectedOutput: fmt.Sprintf(
				"Updated worker group %s in Ray cluster %s in namespace %s (Scaled Replicas: 1 to 7)",
				workerGroup, cluster, testNamespace),
		},
		{
			name:     "should error when replicas greater than max-replicas",
			replicas: desiredReplicas,
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName:   workerGroup,
								Replicas:    ptr.To(int32(3)),
								MinReplicas: ptr.To(int32(1)),
								MaxReplicas: ptr.To(int32(5)),
							},
						},
					},
				},
			},
			expectedError: "cannot set --replicas (7) greater than --max-replicas (5)",
		},
		{
			name:     "should error when min-replicas greater than max-replicas",
			replicas: desiredReplicas,
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName:   workerGroup,
								Replicas:    ptr.To(int32(3)),
								MinReplicas: ptr.To(int32(8)),
								MaxReplicas: ptr.To(int32(5)),
							},
						},
					},
				},
			},
			expectedError: "cannot set --min-replicas (8) greater than --max-replicas (5)",
		},
		{
			name:     "should error when replicas less than min-replicas",
			replicas: 2,
			rayClusters: []runtime.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster,
						Namespace: testNamespace,
					},
					Spec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName:   workerGroup,
								Replicas:    ptr.To(int32(2)),
								MinReplicas: ptr.To(int32(3)),
								MaxReplicas: ptr.To(int32(5)),
							},
						},
					},
				},
			},
			expectedError: "cannot set --replicas (2) less than --min-replicas (3)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeScaleClusterOptions := ScaleClusterOptions{
				cmdFactory:  cmdFactory,
				ioStreams:   &testStreams,
				namespace:   testNamespace,
				cluster:     cluster,
				replicas:    &tc.replicas,
				workerGroup: workerGroup,
			}

			kubeClientSet := kubefake.NewClientset()
			rayClient := rayClientFake.NewSimpleClientset(tc.rayClusters...)
			k8sClients := client.NewClientForTesting(kubeClientSet, rayClient)

			var buf bytes.Buffer
			err := fakeScaleClusterOptions.Run(context.Background(), k8sClients, &buf)

			if tc.expectedError == "" {
				require.NoError(t, err)
				assert.Contains(t, buf.String(), tc.expectedOutput)
			} else {
				assert.ErrorContains(t, err, tc.expectedError)
			}
		})
	}
}

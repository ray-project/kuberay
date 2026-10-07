package ray

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestHandleDeletionRulesReportsUnmatchedTerminalStatus(t *testing.T) {
	recorder := events.NewFakeRecorder(1)
	reconciler := &RayJobReconciler{Recorder: recorder}
	jobStatus := rayv1.JobStatusRunning
	ruleJobStatus := rayv1.JobStatusSucceeded
	jobDeploymentStatus := rayv1.JobDeploymentStatusFailed
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-unmatched-rule",
			Namespace: "default",
		},
		Spec: rayv1.RayJobSpec{
			DeletionStrategy: &rayv1.DeletionStrategy{
				DeletionRules: []rayv1.DeletionRule{{
					Condition: rayv1.DeletionCondition{JobStatus: &ruleJobStatus},
					Policy:    rayv1.DeleteCluster,
				}},
			},
		},
		Status: rayv1.RayJobStatus{
			JobStatus:           jobStatus,
			JobDeploymentStatus: jobDeploymentStatus,
		},
	}

	result, err := reconciler.handleDeletionRules(context.Background(), rayJob)
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "Warning")
		assert.Contains(t, event, string(utils.NoMatchingDeletionRule))
		assert.Contains(t, event, "jobDeploymentStatus=\"Failed\"")
		assert.Contains(t, event, "No cleanup action will be taken")
	default:
		t.Fatal("expected an unmatched deletion rule warning event, got none")
	}
}

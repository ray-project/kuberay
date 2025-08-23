package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestRayJobSubmitComplete(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	fakeSubmitJobOptions := NewJobSubmitOptions(cmdFactory, testStreams)
	fakeSubmitJobOptions.runtimeEnv = "././fake/path/to/env/yaml"
	fakeSubmitJobOptions.fileName = "fake/path/to/rayjob.yaml"

	cmd := &cobra.Command{}
	cmd.Flags().StringVarP(&fakeSubmitJobOptions.namespace, "namespace", "n", "", "")

	err := fakeSubmitJobOptions.Complete(cmd)
	require.NoError(t, err)
	assert.Equal(t, "default", fakeSubmitJobOptions.namespace)
	assert.Equal(t, "fake/path/to/env/yaml", fakeSubmitJobOptions.runtimeEnv)
}

func TestRayJobSubmitWithYamlValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeDir := t.TempDir()

	tests := []struct {
		name        string
		yamlContent string
		expectError string
	}{
		{
			name: "Successful submit job validation with RayJob",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
		},
		{
			name: "BackoffLimit co-exist with InteractiveMode",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  backoffLimit: 1`,
			expectError: "BackoffLimit is incompatible with InteractiveMode",
		},
		{
			name: "BackoffLimit is set to 0 with InteractiveMode",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  backoffLimit: 0`,
		},
		{
			name: "shutdownAfterJobFinishes is false and ttlSecondsAfterFinished is not zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			expectError: "ttlSecondsAfterFinished is only supported when shutdownAfterJobFinishes is set to true",
		},
		{
			name: "shutdownAfterJobFinishes is true and ttlSecondsAfterFinished is not zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
		},
		{
			name: "ttlSecondsAfterFinished is less than zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: -10
  submissionMode: 'InteractiveMode'`,
			expectError: "ttlSecondsAfterFinished must be greater than or equal to 0",
		},
		{
			name: "ttlSecondsAfterFinished is less than zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 0
  submissionMode: 'InteractiveMode'`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

			file, err := os.Create(rayJobYamlPath)
			require.NoError(t, err)
			_, err = file.Write([]byte(tc.yamlContent))
			require.NoError(t, err)

			opts := &SubmitJobOptions{
				cmdFactory: cmdFactory,
				ioStreams:  &testStreams,
				fileName:   rayJobYamlPath,
				workingDir: "Fake/File/Path",
			}

			err = opts.Validate(&cobra.Command{})
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRayJobSubmitWithoutYamlValidate(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	test := []struct {
		name                     string
		rayjobName               string
		expectError              string
		ttlSecondsAfterFinished  int32
		shutdownAfterJobFinishes bool
	}{
		{
			name:                    "ttlSecondsAfterFinished is validate value",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: 10,
		},
		{
			name:                    "ttlSecondsAfterFinished is less than zero",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: -10,
			expectError:             "--ttl-seconds-after-finished must be greater than or equal to 0",
		},
		{
			name:                    "ttlSecondsAfterFinished is equal to zero",
			rayjobName:              "rayjob-sample",
			ttlSecondsAfterFinished: 0,
		},
	}

	for _, tc := range test {
		t.Run(tc.name, func(t *testing.T) {
			opts := &SubmitJobOptions{
				cmdFactory:              cmdFactory,
				ioStreams:               &testStreams,
				rayjobName:              tc.rayjobName,
				workingDir:              "Fake/File/Path",
				ttlSecondsAfterFinished: tc.ttlSecondsAfterFinished,
			}
			err := opts.Validate(&cobra.Command{})
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func getExpectedFieldValue(job rayv1.RayJob, path string) any {
	switch path {
	case "ShutdownAfterJobFinishes":
		return job.Spec.ShutdownAfterJobFinishes
	case "TTLSecondsAfterFinished":
		return job.Spec.TTLSecondsAfterFinished
	case "RayJobName":
		return job.ObjectMeta.Name
	case "RayVersion":
		return job.Spec.RayClusterSpec.RayVersion
	case "headGroupResourceRequestCPU":
		cpu := job.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
		v, _ := cpu.AsInt64()
		return v
	case "headGroupResourceRequestMemory":
		memory := job.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
		return memory.String()
	case "headGroupResourceRequestGPU":
		gpu := job.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests[util.ResourceNvidiaGPU]
		v, _ := gpu.AsInt64()
		return v
	case "workerReplicas":
		return job.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas
	case "workerGroupResourceRequestCPU":
		cpu := job.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
		v, _ := cpu.AsInt64()
		return v
	case "workerGroupResourceRequestMemory":
		memory := job.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
		return memory.String()
	case "workerGroupResourceRequestGPU":
		gpu := job.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[util.ResourceNvidiaGPU]
		v, _ := gpu.AsInt64()
		return v
	case "HeadGroupNodeSelector":
		return job.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.NodeSelector
	case "WorkerGroupNodeSelector":
		return job.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.NodeSelector
	default:
		panic(fmt.Sprintf("unsupported path: %v", path))
	}
}

func TestRayJobSubmitCmdFlagsOverrideSpecYaml(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))

	fakeDir := t.TempDir()

	type ExpectedField struct {
		Expected any
		Field    string
	}

	tests := []struct {
		flagMap      map[string]any
		name         string
		yamlContent  string
		expectError  string
		expectFields []ExpectedField
	}{
		{
			name: "Both shutdownAfterJobFinishes and ttlSecondsAfterFinished are not set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(20)},
			},
		},
		{
			name: "Both shutdownAfterJobFinishes and ttlSecondsAfterFinished are set in yaml with wrong values",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  ttlSecondsAfterFinished: -10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(20)},
			},
		},
		{
			name: "Only shutdownAfterJobFinishes is set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: false
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(20)},
			},
		},
		{
			name: "Only ttlSecondsAfterFinished is set in yaml",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(20)},
			},
		},
		{
			name: "Override only ttlSecondsAfterFinished",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 100
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 200,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(200)},
			},
		},
		{
			name: "Set ttl-seconds-after-finished to zero",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 0,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(0)},
			},
		},
		{
			name: "Override only ttlSecondsAfterFinished to negative and cause error",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  shutdownAfterJobFinishes: true
  ttlSecondsAfterFinished: 10
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": -10,
			},
			expectError: "--ttl-seconds-after-finished must be greater than or equal to 0",
		},
		{
			name: "Override ShutdownAfterJobFinishes and TTLSecondsAfterFinished",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"ttl-seconds-after-finished": 20,
			},
			expectFields: []ExpectedField{
				{Field: "ShutdownAfterJobFinishes", Expected: true},
				{Field: "TTLSecondsAfterFinished", Expected: int32(20)},
			},
		},
		{
			name: "Override metadata.name via flag",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`,
			flagMap: map[string]any{
				"name": "rayjob-sample-override",
			},
			expectFields: []ExpectedField{
				{Field: "RayJobName", Expected: "rayjob-sample-override"},
			},
		},
		{
			name: "Override ray version",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    rayVersion: "2.46.0"`,
			flagMap: map[string]any{
				"ray-version": "1.0.0",
			},
			expectFields: []ExpectedField{
				{Field: "RayVersion", Expected: "1.0.0"},
			},
		},
		{
			name: "Override head-cpu count",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
  headGroupSpec:
    template:
      spec:
        containers:
        - name: ray-head
          image: rayproject/ray:latest
          resources:
            requests:
              cpu: "20"
              memory: "40Gi"
              nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"head-cpu": 40,
			},
			expectFields: []ExpectedField{
				{Field: "headGroupResourceRequestCPU", Expected: int64(40)},
			},
		},
		{
			name: "Override head-memory size",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    headGroupSpec:
      template:
       spec:
        containers:
          - name: ray-head
            image: rayproject/ray:latest
            resources:
             requests:
              cpu: "20"
              memory: "40Gi"
              nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"head-memory": "80Gi",
			},
			expectFields: []ExpectedField{
				{Field: "headGroupResourceRequestMemory", Expected: "80Gi"},
			},
		},
		{
			name: "Override head-gpu count",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    headGroupSpec:
      template:
       spec:
        containers:
          - name: ray-head
            image: rayproject/ray:latest
            resources:
             requests:
              cpu: "20"
              memory: "40Gi"
              nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"head-gpu": 20,
			},
			expectFields: []ExpectedField{
				{Field: "headGroupResourceRequestGPU", Expected: int64(20)},
			},
		},
		{
			name: "Override worker-replicas",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    workerGroupSpecs:
    - groupName: ray-worker-group
      replicas: 2`,
			flagMap: map[string]any{
				"worker-replicas": 5,
			},
			expectFields: []ExpectedField{
				{Field: "workerReplicas", Expected: ptr.To(int32(5))},
			},
		},
		{
			name: "Override worker-cpu count",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    workerGroupSpecs:
      - groupName: ray-worker-group
        template:
          spec:
            containers:
              - name: ray-worker
                image: rayproject/ray:latest
                resources:
                  requests:
                    cpu: "20"
                    memory: "40Gi"
                    nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"worker-cpu": 40,
			},
			expectFields: []ExpectedField{
				{Field: "workerGroupResourceRequestCPU", Expected: int64(40)},
			},
		},
		{
			name: "Override worker-memory size",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    workerGroupSpecs:
      - groupName: ray-worker-group
        template:
          spec:
            containers:
              - name: ray-worker
                image: rayproject/ray:latest
                resources:
                  requests:
                    cpu: "20"
                    memory: "40Gi"
                    nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"worker-memory": "80Gi",
			},
			expectFields: []ExpectedField{
				{Field: "workerGroupResourceRequestMemory", Expected: "80Gi"},
			},
		},
		{
			name: "Override worker-gpu count",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    workerGroupSpecs:
      - groupName: ray-worker-group
        template:
          spec:
            containers:
              - name: ray-worker
                image: rayproject/ray:latest
                resources:
                  requests:
                    cpu: "20"
                    memory: "40Gi"
                    nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"worker-gpu": 20,
			},
			expectFields: []ExpectedField{
				{Field: "workerGroupResourceRequestGPU", Expected: int64(20)},
			},
		},
		{
			name: "Override head and worker resources together",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    headGroupSpec:
      template:
        spec:
          containers:
          - name: ray-head
            image: rayproject/ray:latest
            resources:
              requests:
                cpu: "20"
                memory: "40Gi"
                nvidia.com/gpu: "10"
    workerGroupSpecs:
      - groupName: ray-worker-group
        template:
          spec:
            containers:
              - name: ray-worker
                image: rayproject/ray:latest
                resources:
                  requests:
                    cpu: "20"
                    memory: "40Gi"
                    nvidia.com/gpu: "10"`,
			flagMap: map[string]any{
				"head-cpu":      40,
				"head-memory":   "80Gi",
				"head-gpu":      20,
				"worker-cpu":    40,
				"worker-memory": "80Gi",
				"worker-gpu":    20,
			},
			expectFields: []ExpectedField{
				{Field: "headGroupResourceRequestCPU", Expected: int64(40)},
				{Field: "headGroupResourceRequestMemory", Expected: "80Gi"},
				{Field: "headGroupResourceRequestGPU", Expected: int64(20)},
				{Field: "workerGroupResourceRequestCPU", Expected: int64(40)},
				{Field: "workerGroupResourceRequestMemory", Expected: "80Gi"},
				{Field: "workerGroupResourceRequestGPU", Expected: int64(20)},
			},
		},
		{
			name: "Override worker-node-selectors",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    workerGroupSpecs:
    - groupName: ray-worker-group
      template:
        spec:
          nodeSelector:
            ray.io/ray-node-type: worker`,
			flagMap: map[string]any{
				"worker-node-selectors": map[string]string{
					"ray.io/ray-node-type": "worker1",
					"custom-label":         "custom-value",
				},
			},
			expectFields: []ExpectedField{
				{Field: "WorkerGroupNodeSelector", Expected: map[string]string{
					"ray.io/ray-node-type": "worker1",
					"custom-label":         "custom-value",
				}},
			},
		},
		{
			name: "Override head-node-selectors",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    headGroupSpec:
      template:
        spec:
          containers:
          - name: ray-head
            nodeSelector:
              ray.io/ray-node-type: head`,
			flagMap: map[string]any{
				"head-node-selectors": map[string]string{
					"ray.io/ray-node-type": "head1",
					"custom-label":         "custom-value",
				},
			},
			expectFields: []ExpectedField{
				{Field: "HeadGroupNodeSelector", Expected: map[string]string{
					"ray.io/ray-node-type": "head1",
					"custom-label":         "custom-value",
				}},
			},
		},
		{
			name: "Override head and worker node-selectors together",
			yamlContent: `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'
  rayClusterSpec:
    headGroupSpec:
      template:
        spec:
          containers:
          - name: ray-head
            nodeSelector:
              ray.io/ray-node-type: head
    workerGroupSpecs:
    - groupName: ray-worker-group
      template:
        spec:
          nodeSelector:
            ray.io/ray-node-type: worker`,
			flagMap: map[string]any{
				"head-node-selectors": map[string]string{
					"ray.io/ray-node-type": "head1",
					"custom-head-label":    "custom-head-value",
				},
				"worker-node-selectors": map[string]string{
					"ray.io/ray-node-type": "worker1",
					"custom-worker-label":  "custom-worker-value",
				},
			},
			expectFields: []ExpectedField{
				{Field: "HeadGroupNodeSelector", Expected: map[string]string{
					"ray.io/ray-node-type": "head1",
					"custom-head-label":    "custom-head-value",
				}},
				{Field: "WorkerGroupNodeSelector", Expected: map[string]string{
					"ray.io/ray-node-type": "worker1",
					"custom-worker-label":  "custom-worker-value",
				}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rayJobYamlPath := filepath.Join(fakeDir, "rayjob-temp-*.yaml")

			file, err := os.Create(rayJobYamlPath)
			require.NoError(t, err)
			_, err = file.Write([]byte(tc.yamlContent))
			require.NoError(t, err)

			options := &SubmitJobOptions{
				cmdFactory: cmdFactory,
				ioStreams:  &testStreams,
				fileName:   rayJobYamlPath,
				workingDir: "Fake/File/Path",
			}
			cmd := &cobra.Command{}
			cmd.Flags().StringVar(&options.rayjobName, "name", "", "")
			cmd.Flags().StringVar(&options.rayVersion, "ray-version", util.RayVersion, "")
			cmd.Flags().StringVar(&options.image, "image", fmt.Sprintf("rayproject/ray:%s", options.rayVersion), "")
			cmd.Flags().Int32Var(&options.ttlSecondsAfterFinished, "ttl-seconds-after-finished", 0, "")
			cmd.Flags().StringVar(&options.headCPU, "head-cpu", "2", "")
			cmd.Flags().StringVar(&options.headMemory, "head-memory", "4Gi", "")
			cmd.Flags().StringVar(&options.headGPU, "head-gpu", "0", "")
			cmd.Flags().Int32Var(&options.workerReplicas, "worker-replicas", 1, "")
			cmd.Flags().StringVar(&options.workerCPU, "worker-cpu", "2", "")
			cmd.Flags().StringVar(&options.workerMemory, "worker-memory", "4Gi", "")
			cmd.Flags().StringVar(&options.workerGPU, "worker-gpu", "0", "")
			cmd.Flags().StringToStringVar(&options.headNodeSelectors, "head-node-selectors", nil, "")
			cmd.Flags().StringToStringVar(&options.workerNodeSelectors, "worker-node-selectors", nil, "")

			args := []string{}
			for flag, value := range tc.flagMap {
				switch v := value.(type) {
				case bool:
					if v {
						args = append(args, fmt.Sprintf("--%s", flag))
					}
				case map[string]string:
					var kvPairs []string
					for k, val := range v {
						kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", k, val))
					}
					args = append(args, fmt.Sprintf("--%s=%s", flag, strings.Join(kvPairs, ",")))
				default:
					args = append(args, fmt.Sprintf("--%s=%v", flag, v))
				}
			}

			cmd.SetArgs(args)
			err = cmd.ParseFlags(args)
			require.NoError(t, err)

			err = options.Validate(cmd)
			if tc.expectError != "" {
				require.EqualError(t, err, tc.expectError)
			} else {
				require.NoError(t, err)
			}

			for _, field := range tc.expectFields {
				actual := getExpectedFieldValue(*options.RayJob, field.Field)
				require.Equal(t, field.Expected, actual)
			}
		})
	}
}

func TestDecodeRayJobYaml(t *testing.T) {
	rayjobtmpfile, err := os.CreateTemp("./", "rayjob-temp-*.yaml")
	require.NoError(t, err)

	defer os.Remove(rayjobtmpfile.Name())

	rayYaml := `apiVersion: ray.io/v1
kind: RayJob
metadata:
  name: rayjob-sample
spec:
  submissionMode: 'InteractiveMode'`
	_, err = rayjobtmpfile.Write([]byte(rayYaml))
	require.NoError(t, err)

	rayJobYamlActual, err := decodeRayJobYaml(filepath.Join("./", rayjobtmpfile.Name()))
	require.NoError(t, err)

	assert.Equal(t, "rayjob-sample", rayJobYamlActual.GetName())

	submissionMode := rayJobYamlActual.Spec.SubmissionMode
	assert.Equal(t, rayv1.InteractiveMode, submissionMode)
}

func TestRuntimeEnvHasWorkingDir(t *testing.T) {
	runtimeEnvFile, err := os.CreateTemp("./", "runtime-env-*.yaml")
	require.NoError(t, err)

	defer os.Remove(runtimeEnvFile.Name())

	runTimeEnv := `pip:
  - requests==2.26.0
  - pendulum==2.1.2
env_vars:
  counter_name: "test_counter"
working_dir: /fake/dir/ray_working_dir/
`
	_, err = runtimeEnvFile.Write([]byte(runTimeEnv))
	require.NoError(t, err)

	runtimeEnvActual, err := runtimeEnvHasWorkingDir(filepath.Join("./", runtimeEnvFile.Name()))
	require.NoError(t, err)

	assert.NotEmpty(t, runtimeEnvActual)
	assert.Equal(t, "/fake/dir/ray_working_dir/", runtimeEnvActual)
}

func TestRaySubmitCmd(t *testing.T) {
	testStreams, _, _, _ := genericclioptions.NewTestIOStreams()
	cmdFactory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	fakeSubmitJobOptions := NewJobSubmitOptions(cmdFactory, testStreams)

	fakeSubmitJobOptions.runtimeEnv = "/fake-runtime/path"
	fakeSubmitJobOptions.runtimeEnvJson = "{\"env_vars\":{\"counter_name\":\"test_counter\"}"
	fakeSubmitJobOptions.submissionID = "fake-submission-id12345"
	fakeSubmitJobOptions.entryPointCPU = 2.0
	fakeSubmitJobOptions.entryPointGPU = 1.0
	fakeSubmitJobOptions.entryPointMemory = 600
	fakeSubmitJobOptions.entryPointResource = "{\"fake-resource\":{\"the-fake-resource\"}"
	fakeSubmitJobOptions.noWait = true
	fakeSubmitJobOptions.headers = "{\"requestHeaders\": {\"header\": \"header\"}}"
	fakeSubmitJobOptions.verify = "True"
	fakeSubmitJobOptions.workingDir = "/fake/working/dir"
	fakeSubmitJobOptions.entryPoint = "python fake_python_script.py"

	actualCmd, err := fakeSubmitJobOptions.raySubmitCmd()
	require.NoError(t, err)
	expectedCmd := []string{
		"ray",
		"job",
		"submit",
		"--address",
		dashboardAddr,
		"--runtime-env",
		"/fake-runtime/path",
		"--runtime-env-json",
		"{\"env_vars\":{\"counter_name\":\"test_counter\"}",
		"--submission-id",
		"fake-submission-id12345",
		"--entrypoint-num-cpus",
		"2.000000",
		"--entrypoint-num-gpus",
		"1.000000",
		"--entrypoint-memory",
		"600",
		"--entrypoint-resource",
		"{\"fake-resource\":{\"the-fake-resource\"}",
		"--no-wait",
		"--headers",
		"{\"requestHeaders\": {\"header\": \"header\"}}",
		"--verify",
		"True",
		"--working-dir",
		"/fake/working/dir",
		"--",
		"python",
		"fake_python_script.py",
	}

	assert.Equal(t, expectedCmd, actualCmd)
}

func TestRayJobSubmit_FlagsHaveDefaults(t *testing.T) {
	streams, _, _, _ := genericclioptions.NewTestIOStreams()
	factory := cmdutil.NewFactory(genericclioptions.NewConfigFlags(true))
	opts := NewJobSubmitOptions(factory, streams)

	cmd := NewJobSubmitCommand(factory, streams)
	require.NoError(t, cmd.ParseFlags([]string{}))

	assert.InDelta(t, float32(0), opts.entryPointCPU, 1e-6, "default entrypoint-num-cpus should be 0")
	assert.InDelta(t, float32(0), opts.entryPointGPU, 1e-6, "default entrypoint-num-gpus should be 0")
	assert.Equal(t, 0, opts.entryPointMemory, "default entrypoint-memory should be 0")
	assert.False(t, opts.noWait, "default no-wait should be false")
}

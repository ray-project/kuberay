package job

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/portforward"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/yaml"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayscheme "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
)

const (
	dashboardAddr      = "http://localhost:8265"
	clusterTimeout     = 120.0
	portforwardtimeout = 60.0
)

type SubmitJobOptions struct {
	cmdFactory               cmdutil.Factory
	ioStreams                *genericiooptions.IOStreams
	RayJob                   *rayv1.RayJob
	workerNodeSelectors      map[string]string
	headNodeSelectors        map[string]string
	logColor                 string
	image                    string
	fileName                 string
	workingDir               string
	runtimeEnv               string
	headers                  string
	verify                   string
	cluster                  string
	runtimeEnvJson           string
	entryPointResource       string
	metadataJson             string
	logStyle                 string
	submissionID             string
	rayjobName               string
	rayVersion               string
	entryPoint               string
	headCPU                  string
	headMemory               string
	headGPU                  string
	workerCPU                string
	workerMemory             string
	workerGPU                string
	namespace                string
	entryPointMemory         int
	entryPointGPU            float32
	workerReplicas           int32
	entryPointCPU            float32
	noWait                   bool
	dryRun                   bool
	verbose                  bool
	shutdownAfterJobFinishes bool
	ttlSecondsAfterFinished  int32
}

type JobInfo struct {
	SubmissionID string `json:"submission_id"`
	Entrypoint   string `json:"entrypoint"`
	Type         string `json:"type"`
}

var (
	jobSubmitLong = templates.LongDesc(`
		Submit Ray job to Ray cluster as one would using Ray CLI e.g. 'ray job submit ENTRYPOINT'. Command supports all options that 'ray job submit' supports, except '--address'.
		If Ray cluster is already setup, use 'kubectl ray session' instead.

		If no RayJob YAML file is specified, the command will create a default RayJob for the user.

		Command will apply RayJob CR and also submit the Ray job. RayJob CR is required.
	`)

	jobSubmitExample = templates.Examples(fmt.Sprintf(`
		# Submit Ray job with working-directory
		kubectl ray job submit -f rayjob.yaml --working-dir /path/to/working-dir/ -- python my_script.py

		# Submit Ray job with runtime Env file and working directory
		kubectl ray job submit -f rayjob.yaml --working-dir /path/to/working-dir/ --runtime-env /runtimeEnv.yaml -- python my_script.py

		# Submit Ray job with runtime Env file assuming runtime-env has working_dir set
		kubectl ray job submit -f rayjob.yaml --runtime-env path/to/runtimeEnv.yaml -- python my_script.py

		# Submit generated Ray job with default values and with runtime Env file and working directory
		kubectl ray job submit --name rayjob-sample --working-dir /path/to/working-dir/ --runtime-env /runtimeEnv.yaml -- python my_script.py

		# Submit a Ray job with specific head-node-selectors and worker-node-selectors using kubectl ray job submit
		kubectl ray job submit --name rayjob-sample --working-dir /path/to/working-dir/ --head-node-selectors kubernetes.io/os=linux --worker-node-selectors kubernetes.io/os=linux -- python my_script.py

		# Generate Ray job with specifications and submit Ray job with runtime Env file and working directory
		kubectl ray job submit --name rayjob-sample --ray-version %s --image %s --head-cpu 1 --head-memory 5Gi --head-gpu 1 --worker-replicas 3 --worker-cpu 1 --work-gpu 1 --worker-memory 5Gi --runtime-env path/to/runtimeEnv.yaml -- python my_script.py

		# Generate Ray job with specifications and print out the generated RayJob YAML
		kubectl ray job submit --dry-run --name rayjob-sample --ray-version %s --image %s --head-cpu 1 --head-memory 5Gi --worker-replicas 3 --worker-cpu 1 --worker-memory 5Gi --runtime-env path/to/runtimeEnv.yaml -- python my_script.py
	`, util.RayVersion, util.RayImage, util.RayVersion, util.RayImage))
)

func NewJobSubmitOptions(cmdFactory cmdutil.Factory, streams genericiooptions.IOStreams) *SubmitJobOptions {
	return &SubmitJobOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewJobSubmitCommand(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewJobSubmitOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:     "submit [OPTIONS] -f/--filename RAYJOB_YAML -- ENTRYPOINT",
		Short:   "Submit Ray job to Ray cluster",
		Long:    jobSubmitLong,
		Example: jobSubmitExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			entryPointStart := cmd.ArgsLenAtDash()
			if entryPointStart == -1 || len(args[entryPointStart:]) == 0 {
				return cmdutil.UsageErrorf(cmd, "%s", cmd.Use)
			}
			options.entryPoint = strings.Join(args[entryPointStart:], " ")
			if err := options.Complete(cmd); err != nil {
				return err
			}
			if err := options.Validate(cmd); err != nil {
				return err
			}
			return options.Run(cmd.Context(), cmdFactory)
		},
	}
	cmd.Flags().StringVarP(&options.fileName, "filename", "f", options.fileName, "Path and name of the Ray Job YAML file")
	cmd.Flags().StringVar(&options.submissionID, "submission-id", options.submissionID, "ID to specify for the Ray job. If not provided, one will be generated")
	cmd.Flags().StringVar(&options.runtimeEnv, "runtime-env", options.runtimeEnv, "Path and name to the runtime env YAML file.")
	cmd.Flags().StringVar(&options.workingDir, "working-dir", options.workingDir, "Directory containing files that your job will run in")
	cmd.Flags().StringVar(&options.headers, "headers", options.headers, "Used to pass headers through http/s to Ray Cluster. Must be JSON formatting")
	cmd.Flags().StringVar(&options.runtimeEnvJson, "runtime-env-json", options.runtimeEnvJson, "JSON-serialized runtime_env dictionary. Precedence over Ray job CR.")
	cmd.Flags().StringVar(&options.verify, "verify", options.verify, "Boolean indication to verify the server's TLS certificate or a path to a file or directory of trusted certificates.")
	cmd.Flags().StringVar(&options.entryPointResource, "entrypoint-resources", options.entryPointResource, "JSON-serialized dictionary mapping resource name to resource quantity")
	cmd.Flags().StringVar(&options.metadataJson, "metadata-json", options.metadataJson, "JSON-serialized dictionary of metadata to attach to the job.")
	cmd.Flags().StringVar(&options.logStyle, "log-style", options.logStyle, "Specific to 'ray job submit'. Options are 'auto | record | pretty'")
	cmd.Flags().StringVar(&options.logColor, "log-color", options.logColor, "Specific to 'ray job submit'. Options are 'auto | false | true'")
	cmd.Flags().Float32Var(&options.entryPointCPU, "entrypoint-num-cpus", options.entryPointCPU, "Number of CPUs reserved for the for the entrypoint command")
	cmd.Flags().Float32Var(&options.entryPointGPU, "entrypoint-num-gpus", options.entryPointGPU, "Number of GPUs reserved for the for the entrypoint command")
	cmd.Flags().IntVar(&options.entryPointMemory, "entrypoint-memory", options.entryPointMemory, "Amount of memory reserved for the entrypoint command")
	cmd.Flags().BoolVar(&options.noWait, "no-wait", options.noWait, "If present, will not stream logs and wait for job to finish")

	cmd.Flags().StringVar(&options.rayjobName, "name", "", "Ray job name")
	cmd.Flags().StringVar(&options.rayVersion, "ray-version", util.RayVersion, "Ray version to use")
	cmd.Flags().StringVar(&options.image, "image", fmt.Sprintf("rayproject/ray:%s", options.rayVersion), "container image to use")
	cmd.Flags().StringVar(&options.headCPU, "head-cpu", "2", "number of CPUs in the Ray head")
	cmd.Flags().StringVar(&options.headMemory, "head-memory", "4Gi", "amount of memory in the Ray head")
	cmd.Flags().StringVar(&options.headGPU, "head-gpu", "0", "number of GPUs in the Ray head")
	cmd.Flags().Int32Var(&options.workerReplicas, "worker-replicas", 1, "desired worker group replicas")
	cmd.Flags().StringVar(&options.workerCPU, "worker-cpu", "2", "number of CPUs in each worker group replica")
	cmd.Flags().StringVar(&options.workerMemory, "worker-memory", "4Gi", "amount of memory in each worker group replica")
	cmd.Flags().StringVar(&options.workerGPU, "worker-gpu", "0", "number of GPUs in each worker group replica")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "print the generated YAML instead of creating the cluster. Only works when filename is not provided")
	cmd.Flags().BoolVarP(&options.verbose, "verbose", "v", false, "Passing the '--verbose' flag to the 'ray job submit' command")
	cmd.Flags().StringToStringVar(&options.headNodeSelectors, "head-node-selectors", nil, "Node selectors to apply to the head pod in the cluster (e.g. --head-node-selectors topology.kubernetes.io/zone=us-east-1c)")
	cmd.Flags().StringToStringVar(&options.workerNodeSelectors, "worker-node-selectors", nil, "Node selectors to apply to all worker pods in the cluster (e.g. --worker-node-selectors topology.kubernetes.io/zone=us-east-1c)")
	cmd.Flags().Int32Var(&options.ttlSecondsAfterFinished, "ttl-seconds-after-finished", 0, "TTL seconds after finished.")

	return cmd
}

func (options *SubmitJobOptions) Complete(cmd *cobra.Command) error {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}
	options.namespace = namespace
	if options.namespace == "" {
		options.namespace = "default"
	}

	if len(options.runtimeEnv) > 0 {
		options.runtimeEnv = filepath.Clean(options.runtimeEnv)
	}

	if options.fileName != "" {
		options.fileName = filepath.Clean(options.fileName)
	}
	return nil
}

func (options *SubmitJobOptions) Validate(cmd *cobra.Command) error {
	if len(options.runtimeEnv) > 0 {
		info, err := os.Stat(options.runtimeEnv)
		if os.IsNotExist(err) {
			return fmt.Errorf("Runtime Env file does not exist. Failed with: %w", err)
		} else if err != nil {
			return fmt.Errorf("Error occurred when checking runtime env file: %w", err)
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("Filename given is not a regular file. Failed with: %w", err)
		}

		runtimeEnvWorkingDir, err := runtimeEnvHasWorkingDir(options.runtimeEnv)
		if err != nil {
			return fmt.Errorf("Error while checking runtime env: %w", err)
		}
		if len(runtimeEnvWorkingDir) > 0 && options.workingDir == "" {
			options.workingDir = runtimeEnvWorkingDir
		}
	}

	if cmd.Flags().Changed("ttl-seconds-after-finished") {
		options.shutdownAfterJobFinishes = true
	}

	if options.ttlSecondsAfterFinished < 0 {
		return fmt.Errorf("--ttl-seconds-after-finished must be greater than or equal to 0")
	}

	// Take care of case where there is a filename input
	if options.fileName != "" {
		info, err := os.Stat(options.fileName)
		if os.IsNotExist(err) {
			return fmt.Errorf("Ray Job file does not exist. Failed with: %w", err)
		} else if err != nil {
			return fmt.Errorf("Error occurred when checking Ray job file: %w", err)
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("Filename given is not a regular file. Failed with: %w", err)
		}

		options.RayJob, err = decodeRayJobYaml(options.fileName)
		if err != nil {
			return fmt.Errorf("Failed to decode RayJob Yaml: %w", err)
		}

		submissionMode := options.RayJob.Spec.SubmissionMode
		if submissionMode != rayv1.InteractiveMode {
			return fmt.Errorf("Submission mode of the Ray Job must be set to 'InteractiveMode'")
		}
		// InteractiveMode does not support backoffLimit > 1.
		// When a RayJob fails (e.g., due to a missing script) and retries,
		// spec.JobId remains set, causing the new job to incorrectly transition
		// to Running instead of Waiting or Failed.
		// After discussion, we decided to disallow retries in InteractiveMode
		// to avoid ambiguous state handling and unintended behavior.
		// https://github.com/ray-project/kuberay/issues/3525
		if submissionMode == rayv1.InteractiveMode && options.RayJob.Spec.BackoffLimit != nil && *options.RayJob.Spec.BackoffLimit > 0 {
			return fmt.Errorf("BackoffLimit is incompatible with InteractiveMode")
		}

		runtimeEnvYaml := options.RayJob.Spec.RuntimeEnvYAML
		if options.runtimeEnv == "" && options.runtimeEnvJson == "" && runtimeEnvYaml != "" {
			runtimeJson, err := yaml.YAMLToJSON([]byte(runtimeEnvYaml))
			if err != nil {
				return fmt.Errorf("Failed to convert runtime env to json: %w", err)
			}
			options.runtimeEnvJson = string(runtimeJson)
		}

		if cmd.Flags().Changed("ttl-seconds-after-finished") {
			options.RayJob.Spec.TTLSecondsAfterFinished = options.ttlSecondsAfterFinished
			options.RayJob.Spec.ShutdownAfterJobFinishes = options.shutdownAfterJobFinishes
		}

		if options.RayJob.Spec.TTLSecondsAfterFinished < 0 {
			return fmt.Errorf("ttlSecondsAfterFinished must be greater than or equal to 0")
		}
		if !options.RayJob.Spec.ShutdownAfterJobFinishes && options.RayJob.Spec.TTLSecondsAfterFinished > 0 {
			return fmt.Errorf("ttlSecondsAfterFinished is only supported when shutdownAfterJobFinishes is set to true")
		}
	} else if strings.TrimSpace(options.rayjobName) == "" {
		return fmt.Errorf("Must set either yaml file (--filename) or set Ray job name (--name)")
	}

	if options.workingDir == "" {
		return fmt.Errorf("working directory is required, use --working-dir or set with runtime env")
	}

	resourceFields := map[string]string{
		"head-cpu":      options.headCPU,
		"head-gpu":      options.headGPU,
		"head-memory":   options.headMemory,
		"worker-cpu":    options.workerCPU,
		"worker-gpu":    options.workerGPU,
		"worker-memory": options.workerMemory,
	}

	for name, value := range resourceFields {
		if err := util.ValidateResourceQuantity(value, name); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	return nil
}

func (options *SubmitJobOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	k8sClients, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to initialize clientset: %w", err)
	}

	if options.fileName == "" {
		// Genarate the Ray job.
		rayJobObject := generation.RayJobYamlObject{
			RayJobName:               options.rayjobName,
			Namespace:                options.namespace,
			ShutdownAfterJobFinishes: options.shutdownAfterJobFinishes,
			TTLSecondsAfterFinished:  options.ttlSecondsAfterFinished,
			SubmissionMode:           "InteractiveMode",
			// Prior to kuberay 1.2.2, the entry point is required. To maintain
			// backwards compatibility with 1.2.x, we submit the entry point
			// here, even though it will be ignored.
			// See https://github.com/ray-project/kuberay/issues/3126.
			Entrypoint: options.entryPoint,
			RayClusterConfig: generation.RayClusterConfig{
				RayVersion: &options.rayVersion,
				Image:      &options.image,
				Head: &generation.Head{
					CPU:           &options.headCPU,
					Memory:        &options.headMemory,
					GPU:           &options.headGPU,
					NodeSelectors: options.headNodeSelectors,
				},
				WorkerGroups: []generation.WorkerGroup{
					{
						CPU:           &options.workerCPU,
						Memory:        &options.workerMemory,
						GPU:           &options.workerGPU,
						Replicas:      options.workerReplicas,
						NodeSelectors: options.workerNodeSelectors,
					},
				},
			},
		}
		rayJobApplyConfig := rayJobObject.GenerateRayJobApplyConfig()

		// Print out the yaml if it is a dry run
		if options.dryRun {
			resultYaml, err := generation.ConvertRayJobApplyConfigToYaml(rayJobApplyConfig)
			if err != nil {
				return fmt.Errorf("Failed to convert RayJob into yaml format: %w", err)
			}

			fmt.Printf("%s\n", resultYaml)
			return nil
		}

		// Apply the generated yaml
		rayJobApplyConfigResult, err := k8sClients.RayClient().RayV1().RayJobs(options.namespace).Apply(ctx, rayJobApplyConfig, v1.ApplyOptions{FieldManager: util.FieldManager})
		if err != nil {
			return fmt.Errorf("Failed to apply generated YAML: %w", err)
		}
		options.RayJob = &rayv1.RayJob{}
		options.RayJob.SetName(rayJobApplyConfigResult.Name)
	} else {
		options.RayJob, err = k8sClients.RayClient().RayV1().RayJobs(options.namespace).Create(ctx, options.RayJob, v1.CreateOptions{FieldManager: util.FieldManager})
		if err != nil {
			return fmt.Errorf("Error when creating RayJob CR: %w", err)
		}
	}
	fmt.Printf("Submitted RayJob %s.\n", options.RayJob.GetName())

	if len(options.RayJob.GetName()) > 0 {
		// Add timeout?
		for len(options.RayJob.Status.RayClusterName) == 0 {
			options.RayJob, err = k8sClients.RayClient().RayV1().RayJobs(options.namespace).Get(ctx, options.RayJob.GetName(), v1.GetOptions{})
			if err != nil {
				return fmt.Errorf("Failed to get Ray Job status")
			}
			time.Sleep(2 * time.Second)
		}
		options.cluster = options.RayJob.Status.RayClusterName
	} else {
		return fmt.Errorf("Unknown cluster and did not provide Ray Job. One of the fields must be set")
	}

	// Wait til the cluster is ready
	var clusterReady bool
	clusterWaitStartTime := time.Now()
	currTime := clusterWaitStartTime
	fmt.Printf("Waiting for RayCluster\n")
	fmt.Printf("Checking Cluster Status for cluster %s...\n", options.cluster)
	for !clusterReady && currTime.Sub(clusterWaitStartTime).Seconds() <= clusterTimeout {
		time.Sleep(2 * time.Second)
		currCluster, err := k8sClients.RayClient().RayV1().RayClusters(options.namespace).Get(ctx, options.cluster, v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Failed to get cluster information with error: %w", err)
		}
		clusterReady = isRayClusterReady(currCluster)
		if !clusterReady {
			fmt.Println("Cluster is not ready")
		}
		currTime = time.Now()
	}

	if !clusterReady {
		fmt.Printf("Deleting RayJob...\n")
		err = k8sClients.RayClient().RayV1().RayJobs(options.namespace).Delete(ctx, options.RayJob.GetName(), v1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("Failed to clean up Ray job after time out.: %w", err)
		}
		fmt.Printf("Cleaned Up RayJob: %s\n", options.RayJob.GetName())

		return fmt.Errorf("Timed out waiting for cluster")
	}

	svcName, err := k8sClients.GetRayHeadSvcName(ctx, options.namespace, util.RayCluster, options.cluster)
	if err != nil {
		return fmt.Errorf("Failed to find service name: %w", err)
	}

	// start port forward section
	portForwardCmd := portforward.NewCmdPortForward(factory, *options.ioStreams)
	portForwardCmd.SetArgs([]string{"service/" + svcName, fmt.Sprintf("%d:%d", 8265, 8265)})

	// create new context for port-forwarding so we can cancel the context to stop the port forwarding only
	portforwardctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		fmt.Printf("Port Forwarding service %s\n", svcName)
		if err := portForwardCmd.ExecuteContext(portforwardctx); err != nil {
			log.Fatalf("Error occurred while port-forwarding Ray dashboard: %v", err)
		}
	}()

	// Wait for port forward to be ready
	var portforwardReady bool
	portforwardWaitStartTime := time.Now()
	currTime = portforwardWaitStartTime

	portforwardCheckRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardAddr, nil)
	if err != nil {
		return fmt.Errorf("Error occurred when trying to create request to probe cluster endpoint: %w", err)
	}
	httpClient := http.Client{
		Timeout: 5 * time.Second,
	}
	fmt.Printf("Waiting for portforwarding...")
	for !portforwardReady && currTime.Sub(portforwardWaitStartTime).Seconds() <= portforwardtimeout {
		time.Sleep(2 * time.Second)
		rayDashboardResponse, err := httpClient.Do(portforwardCheckRequest)
		if err != nil {
			err = fmt.Errorf("Error occurred when waiting for portforwarding: %w", err)
			fmt.Println(err)
		}
		if rayDashboardResponse.StatusCode >= 200 && rayDashboardResponse.StatusCode < 300 {
			portforwardReady = true
		}
		rayDashboardResponse.Body.Close()
		currTime = time.Now()
	}
	if !portforwardReady {
		return fmt.Errorf("Timed out waiting for port forwarding")
	}
	fmt.Printf("Portforwarding started on %s\n", dashboardAddr)

	// If submission ID is not provided by the user, generate one.
	if options.submissionID == "" {
		generatedID, err := generateSubmissionID()
		if err != nil {
			return fmt.Errorf("failed to generate submission ID: %w", err)
		}
		options.submissionID = generatedID
		fmt.Printf("Generated submission ID for Ray job: %s\n", options.submissionID)
	}

	// Submitting ray job to cluster
	raySubmitCmd, err := options.raySubmitCmd()
	if err != nil {
		return fmt.Errorf("failed to create Ray submit command with error: %w", err)
	}
	fmt.Printf("Ray command: %v\n", raySubmitCmd)
	cmd := exec.Command(raySubmitCmd[0], raySubmitCmd[1:]...) //nolint:gosec // command is sanitized in raySubmitCmd() and file paths are cleaned in Complete()

	// Get the outputs/pipes for `ray job submit` outputs
	rayCmdStdOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Error while setting up `ray job submit` stdout: %w", err)
	}
	rayCmdStdErr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("Error while setting up `ray job submit` stderr: %w", err)
	}

	go func() {
		fmt.Printf("Running Ray submit job command...\n")
		err := cmd.Start()
		if err != nil {
			log.Fatalf("error occurred while running command %s: %v", fmt.Sprint(raySubmitCmd), err)
		}
	}()

	rayJobID := options.submissionID

	rayCmdStdOutScanner := bufio.NewScanner(rayCmdStdOut)
	rayCmdStdErrScanner := bufio.NewScanner(rayCmdStdErr)
	go func() {
		for {
			currStdToken := rayCmdStdOutScanner.Text()
			if currStdToken != "" {
				fmt.Println(currStdToken)
			}
			scanNotDone := rayCmdStdOutScanner.Scan()
			if !scanNotDone {
				break
			}
		}
	}()
	go func() {
		for {
			currErrToken := rayCmdStdErrScanner.Text()
			if currErrToken != "" {
				fmt.Fprintf(options.ioStreams.ErrOut, "%s\n", currErrToken)
			}
			scanNotDone := rayCmdStdErrScanner.Scan()
			if !scanNotDone {
				break
			}
		}
	}()

	// Add annotation to RayJob with the correct Ray job ID and update the CR
	options.RayJob, err = k8sClients.RayClient().RayV1().RayJobs(options.namespace).Get(ctx, options.RayJob.GetName(), v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to get latest version of Ray job: %w", err)
	}
	options.RayJob.Spec.JobId = rayJobID

	_, err = k8sClients.RayClient().RayV1().RayJobs(options.namespace).Update(ctx, options.RayJob, v1.UpdateOptions{FieldManager: util.FieldManager})
	if err != nil {
		return fmt.Errorf("Error occurred when trying to add job ID to RayJob: %w", err)
	}

	// Wait for Ray job submit to finish.
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("Error occurred with Ray job submit: %w", err)
	}
	if options.noWait {
		fmt.Printf("Ray job submitted with ID %s\n", rayJobID)
		return nil
	}
	// Wait for the Ray job to finish
	watcher, err := k8sClients.RayClient().RayV1().
		RayJobs(options.namespace).
		Watch(ctx, v1.ListOptions{
			FieldSelector: "metadata.name=" + options.RayJob.GetName(),
		})
	if err != nil {
		return fmt.Errorf("failed to watch RayJob: %w", err)
	}
	defer watcher.Stop()

	fmt.Println("Waiting for job to finish...")
	for evt := range watcher.ResultChan() {
		job, ok := evt.Object.(*rayv1.RayJob)
		if !ok {
			fmt.Fprintf(options.ioStreams.ErrOut, "unexpected watch event type %T\n", evt.Object)
			continue
		}

		status := job.Status.JobStatus
		jobID := job.Status.JobId
		if jobID == "" {
			jobID = "unknown"
		}
		fmt.Printf("Current status: %s (RayJob: %s, JobID: %s)\n",
			status, job.GetName(), jobID)

		if rayv1.IsJobTerminal(status) {
			switch status {
			case rayv1.JobStatusSucceeded, rayv1.JobStatusStopped:
				fmt.Printf("Job %s finished with status %s.\n", jobID, status)
				return nil

			case rayv1.JobStatusFailed:
				if msg := job.Status.Message; msg != "" {
					return fmt.Errorf("job %s failed: %s", jobID, msg)
				}
				return fmt.Errorf("job %s failed with status %s", jobID, status)

			default:
				return fmt.Errorf("job %s in unexpected terminal state %s", jobID, status)
			}
		}
	}

	fmt.Fprintf(options.ioStreams.ErrOut,
		"rayjob %s watch ended without a clear terminal state\n", options.RayJob.GetName())
	return nil
}

func (options *SubmitJobOptions) raySubmitCmd() ([]string, error) {
	raySubmitCmd := []string{"ray", "job", "submit", "--address", dashboardAddr}

	if len(options.runtimeEnv) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--runtime-env", options.runtimeEnv)
	}
	if len(options.runtimeEnvJson) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--runtime-env-json", options.runtimeEnvJson)
	}
	if len(options.submissionID) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--submission-id", options.submissionID)
	}
	if options.entryPointCPU > 0 {
		raySubmitCmd = append(raySubmitCmd, "--entrypoint-num-cpus", fmt.Sprintf("%f", options.entryPointCPU))
	}
	if options.entryPointGPU > 0 {
		raySubmitCmd = append(raySubmitCmd, "--entrypoint-num-gpus", fmt.Sprintf("%f", options.entryPointGPU))
	}
	if options.entryPointMemory > 0 {
		raySubmitCmd = append(raySubmitCmd, "--entrypoint-memory", fmt.Sprintf("%d", options.entryPointMemory))
	}
	if len(options.entryPointResource) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--entrypoint-resource", options.entryPointResource)
	}
	if len(options.metadataJson) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--metadata-json", options.metadataJson)
	}
	if options.noWait {
		raySubmitCmd = append(raySubmitCmd, "--no-wait")
	}
	if len(options.headers) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--headers", options.headers)
	}
	if len(options.verify) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--verify", options.verify)
	}
	if len(options.logStyle) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--log-style", options.logStyle)
	}
	if len(options.logColor) > 0 {
		raySubmitCmd = append(raySubmitCmd, "--log-color", options.logColor)
	}
	if options.verbose {
		raySubmitCmd = append(raySubmitCmd, "--verbose")
	}

	raySubmitCmd = append(raySubmitCmd, "--working-dir", options.workingDir)

	raySubmitCmd = append(raySubmitCmd, "--")
	// Sanitize entrypoint
	entryPointSanitized, err := shlex.Split(options.entryPoint)
	if err != nil {
		return nil, err
	}
	raySubmitCmd = append(raySubmitCmd, entryPointSanitized...)

	return raySubmitCmd, nil
}

// Decode RayJob YAML if we decide to submit job using kube client
func decodeRayJobYaml(rayJobFilePath string) (*rayv1.RayJob, error) {
	decodedRayJob := &rayv1.RayJob{}

	rayJobYamlContent, err := os.ReadFile(rayJobFilePath)
	if err != nil {
		return nil, err
	}
	decoder := rayscheme.Codecs.UniversalDecoder()

	_, _, err = decoder.Decode(rayJobYamlContent, nil, decodedRayJob)
	if err != nil {
		return nil, err
	}

	return decodedRayJob, nil
}

func runtimeEnvHasWorkingDir(runtimePath string) (string, error) {
	runtimeEnvFileContent, err := os.ReadFile(runtimePath)
	if err != nil {
		return "", err
	}

	var runtimeEnvYaml map[string]interface{}
	err = yaml.Unmarshal(runtimeEnvFileContent, &runtimeEnvYaml)
	if err != nil {
		return "", err
	}

	if workingDir, ok := runtimeEnvYaml["working_dir"].(string); ok {
		return workingDir, nil
	}

	return "", nil
}

func isRayClusterReady(rayCluster *rayv1.RayCluster) bool {
	return meta.IsStatusConditionTrue(rayCluster.Status.Conditions, "Ready") || rayCluster.Status.State == rayv1.Ready
}

// Generates a 16-character random ID with a prefix, mimicking Ray Job submission_id.
// ref: ray/python/ray/dashboard/modules/job/job_manager.py
func generateSubmissionID() (string, error) {
	// ASCII letters and digits, excluding confusing characters I, l, O, 0, o.
	const possibleChars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ123456789"

	idRunes := make([]rune, 16)
	for i := range idRunes {
		// Securely generate a random index.
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(possibleChars))))
		if err != nil {
			return "", err
		}
		idRunes[i] = rune(possibleChars[idx.Int64()])
	}
	return fmt.Sprintf("raysubmit_%s", string(idRunes)), nil
}

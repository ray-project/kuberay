package create

import (
	"context"
	"fmt"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type CreateClusterOptions struct {
	cmdFactory             cmdutil.Factory
	ioStreams              *genericclioptions.IOStreams
	labels                 map[string]string
	annotations            map[string]string
	workerRayStartParams   map[string]string
	headRayStartParams     map[string]string
	headNodeSelectors      map[string]string
	workerNodeSelectors    map[string]string
	namespace              string
	clusterName            string
	rayVersion             string
	image                  string
	headCPU                string
	headMemory             string
	headEphemeralStorage   string
	headGPU                string
	workerCPU              string
	workerMemory           string
	workerEphemeralStorage string
	workerGPU              string
	workerTPU              string
	timeout                time.Duration
	numOfHosts             int32
	workerReplicas         int32
	dryRun                 bool
	wait                   bool
}

var (
	defaultProvisionedTimeout = 5 * time.Minute

	createClusterExample = templates.Examples(fmt.Sprintf(`
		# Create a Ray cluster using default values
		kubectl ray create cluster sample-cluster

		# Create a Ray cluster from flags input
		kubectl ray create cluster sample-cluster --ray-version %s --image %s --head-cpu 1 --head-memory 5Gi --head-ephemeral-storage 10Gi --worker-replicas 3 --worker-cpu 1 --worker-memory 5Gi --worker-ephemeral-storage 10Gi

		# Create a Ray cluster with K8s labels and annotations
		kubectl ray create cluster sample-cluster --labels app=ray,env=dev --annotations ttl-hours=24,owner=chthulu
	`, util.RayVersion, util.RayImage))
)

func NewCreateClusterOptions(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *CreateClusterOptions {
	return &CreateClusterOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewCreateClusterCommand(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewCreateClusterOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:          "cluster [CLUSTERNAME]",
		Short:        "Create Ray cluster",
		Example:      createClusterExample,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(cmd, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}

			k8sClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			return options.Run(cmd.Context(), k8sClient)
		},
	}

	cmd.Flags().StringVar(&options.rayVersion, "ray-version", util.RayVersion, "Ray version to use")
	cmd.Flags().StringVar(&options.image, "image", fmt.Sprintf("rayproject/ray:%s", options.rayVersion), "container image to use")
	cmd.Flags().StringVar(&options.headCPU, "head-cpu", "2", "number of CPUs in the Ray head")
	cmd.Flags().StringVar(&options.headMemory, "head-memory", "4Gi", "amount of memory in the Ray head")
	cmd.Flags().StringVar(&options.headGPU, "head-gpu", "0", "number of GPUs in the Ray head")
	cmd.Flags().StringVar(&options.headEphemeralStorage, "head-ephemeral-storage", "", "amount of ephemeral storage in the Ray head")
	cmd.Flags().StringToStringVar(&options.headRayStartParams, "head-ray-start-params", options.headRayStartParams, "a map of arguments to the Ray head's 'ray start' entrypoint, e.g. '--head-ray-start-params dashboard-host=0.0.0.0,num-cpus=2'")
	cmd.Flags().Int32Var(&options.workerReplicas, "worker-replicas", 1, "desired worker group replicas")
	cmd.Flags().Int32Var(&options.numOfHosts, "num-of-hosts", 1, "number of hosts in default worker group per replica")
	cmd.Flags().StringVar(&options.workerCPU, "worker-cpu", "2", "number of CPUs in each worker group replica")
	cmd.Flags().StringVar(&options.workerMemory, "worker-memory", "4Gi", "amount of memory in each worker group replica")
	cmd.Flags().StringVar(&options.workerGPU, "worker-gpu", "0", "number of GPUs in each worker group replica")
	cmd.Flags().StringVar(&options.workerTPU, "worker-tpu", "0",
		fmt.Sprintf(
			"number of TPUs in each worker group replica (if set > 0, also specify --worker-node-selectors with %s and %s)",
			util.NodeSelectorGKETPUAccelerator,
			util.NodeSelectorGKETPUTopology,
		),
	)
	cmd.Flags().StringVar(&options.workerEphemeralStorage, "worker-ephemeral-storage", "", "amount of ephemeral storage in each worker group replica")
	cmd.Flags().StringToStringVar(&options.workerRayStartParams, "worker-ray-start-params", options.workerRayStartParams, "a map of arguments to the Ray workers' 'ray start' entrypoint, e.g. '--worker-ray-start-params metrics-export-port=8080,num-cpus=2'")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "print the generated YAML instead of creating the cluster")
	cmd.Flags().BoolVar(&options.wait, "wait", false, "wait for the cluster to be provisioned before returning. Returns an error if the cluster is not provisioned by the timeout specified")
	cmd.Flags().DurationVar(&options.timeout, "timeout", defaultProvisionedTimeout, "the timeout for --wait")
	cmd.Flags().StringToStringVar(&options.headNodeSelectors, "head-node-selectors", nil, "Node selectors to apply to all head pods in the cluster (e.g. --head-node-selectors cloud.google.com/gke-accelerator=nvidia-l4,cloud.google.com/gke-nodepool=my-node-pool)")
	cmd.Flags().StringToStringVar(&options.workerNodeSelectors, "worker-node-selectors", nil, "Node selectors to apply to all worker pods in the cluster (e.g. --worker-node-selectors cloud.google.com/gke-accelerator=nvidia-l4,cloud.google.com/gke-nodepool=my-node-pool)")
	cmd.Flags().StringToStringVar(&options.labels, "labels", nil, "K8s labels (e.g. --labels app=ray,env=dev)")
	cmd.Flags().StringToStringVar(&options.annotations, "annotations", nil, "K8s annotations (e.g. --annotations ttl-hours=24,owner=chthulu)")

	return cmd
}

func (options *CreateClusterOptions) Complete(cmd *cobra.Command, args []string) error {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}
	options.namespace = namespace

	if options.namespace == "" {
		options.namespace = "default"
	}

	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "%s", cmd.Use)
	}
	options.clusterName = args[0]

	if options.image == "" {
		options.image = fmt.Sprintf("rayproject/ray:%s", options.rayVersion)
	}

	return nil
}

func (options *CreateClusterOptions) Validate() error {
	resourceFields := map[string]string{
		"head-cpu":                 options.headCPU,
		"head-gpu":                 options.headGPU,
		"head-memory":              options.headMemory,
		"head-ephemeral-storage":   options.headEphemeralStorage,
		"worker-cpu":               options.workerCPU,
		"worker-gpu":               options.workerGPU,
		"worker-tpu":               options.workerTPU,
		"worker-memory":            options.workerMemory,
		"worker-ephemeral-storage": options.workerEphemeralStorage,
	}

	for name, value := range resourceFields {
		if (name == "head-ephemeral-storage" || name == "worker-ephemeral-storage") && value == "" {
			continue
		}
		if err := util.ValidateResourceQuantity(value, name); err != nil {
			return fmt.Errorf("%w", err)
		}
	}
	// we must assign gke-tpu-accelerator and gke-tpu-topology in nodeSelector
	// if worker-tpu is not 0
	if options.workerTPU != "0" {
		if err := util.ValidateTPUNodeSelector(options.numOfHosts, options.workerNodeSelectors); err != nil {
			return fmt.Errorf("%w", err)
		}
	}
	return nil
}

func (options *CreateClusterOptions) Run(ctx context.Context, k8sClient client.Client) error {
	if clusterExists(k8sClient.RayClient(), options.namespace, options.clusterName) {
		return fmt.Errorf("the Ray cluster %s in namespace %s already exists", options.clusterName, options.namespace)
	}

	rayClusterSpecObject := generation.RayClusterSpecObject{
		Namespace:            &options.namespace,
		Name:                 &options.clusterName,
		Labels:               options.labels,
		Annotations:          options.annotations,
		RayVersion:           &options.rayVersion,
		Image:                &options.image,
		HeadCPU:              &options.headCPU,
		HeadMemory:           &options.headMemory,
		HeadEphemeralStorage: &options.headEphemeralStorage,
		HeadGPU:              &options.headGPU,
		HeadRayStartParams:   options.headRayStartParams,
		HeadNodeSelectors:    options.headNodeSelectors,
		WorkerGroups: []generation.WorkerGroupConfig{
			{
				Name:                   ptr.To("default-group"),
				WorkerReplicas:         &options.workerReplicas,
				NumOfHosts:             &options.numOfHosts,
				WorkerCPU:              &options.workerCPU,
				WorkerMemory:           &options.workerMemory,
				WorkerEphemeralStorage: &options.workerEphemeralStorage,
				WorkerGPU:              &options.workerGPU,
				WorkerTPU:              &options.workerTPU,
				WorkerRayStartParams:   options.workerRayStartParams,
				WorkerNodeSelectors:    options.workerNodeSelectors,
			},
		},
	}

	rayClusterac := rayClusterSpecObject.GenerateRayClusterApplyConfig()

	// If dry run is enabled, it will call the YAML converter and print out the YAML
	if options.dryRun {
		rayClusterYaml, err := generation.ConvertRayClusterApplyConfigToYaml(rayClusterac)
		if err != nil {
			return fmt.Errorf("error creating RayCluster YAML: %w", err)
		}
		fmt.Printf("%s\n", rayClusterYaml)
		return nil
	}

	// TODO: Decide whether to save YAML to file or not.

	result, err := k8sClient.RayClient().RayV1().RayClusters(options.namespace).Apply(ctx, rayClusterac, metav1.ApplyOptions{FieldManager: util.FieldManager})
	if err != nil {
		return fmt.Errorf("failed to create Ray cluster: %w", err)
	}
	fmt.Printf("Created Ray cluster: %s\n", result.GetName())

	if options.wait {
		err = k8sClient.WaitRayClusterProvisioned(ctx, options.namespace, result.GetName(), options.timeout)
		if err != nil {
			return err
		}
		fmt.Printf("Ray cluster %s is provisioned\n", result.GetName())
	}

	return nil
}

// clusterExists checks if a RayCluster with the given name exists in the given namespace
func clusterExists(client rayclient.Interface, namespace, name string) bool {
	if _, err := client.RayV1().RayClusters(namespace).Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		return true
	}
	return false
}

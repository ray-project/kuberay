package create

import (
	"context"
	"fmt"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type CreateClusterOptions struct {
	configFlags    *genericclioptions.ConfigFlags
	ioStreams      *genericclioptions.IOStreams
	kubeContexter  util.KubeContexter
	clusterName    string
	rayVersion     string
	image          string
	headCPU        string
	headMemory     string
	headGPU        string
	workerCPU      string
	workerMemory   string
	workerGPU      string
	workerReplicas int32
	dryRun         bool
	wait           bool
	timeout        time.Duration
}

var (
	defaultProvisionedTimeout = 5 * time.Minute

	createClusterExample = templates.Examples(fmt.Sprintf(`
		# Create a Ray cluster using default values
		kubectl ray create cluster sample-cluster

		# Create a Ray cluster from flags input
		kubectl ray create cluster sample-cluster --ray-version %s --image %s --head-cpu 1 --head-memory 5Gi --worker-replicas 3 --worker-cpu 1 --worker-memory 5Gi
	`, util.RayVersion, util.RayImage))
)

func NewCreateClusterOptions(streams genericclioptions.IOStreams) *CreateClusterOptions {
	return &CreateClusterOptions{
		configFlags:   genericclioptions.NewConfigFlags(true),
		ioStreams:     &streams,
		kubeContexter: &util.DefaultKubeContexter{},
	}
}

func NewCreateClusterCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewCreateClusterOptions(streams)
	cmdFactory := cmdutil.NewFactory(options.configFlags)

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
	cmd.Flags().Int32Var(&options.workerReplicas, "worker-replicas", 1, "desired worker group replicas")
	cmd.Flags().StringVar(&options.workerCPU, "worker-cpu", "2", "number of CPUs in each worker group replica")
	cmd.Flags().StringVar(&options.workerMemory, "worker-memory", "4Gi", "amount of memory in each worker group replica")
	cmd.Flags().StringVar(&options.workerGPU, "worker-gpu", "0", "number of GPUs in each worker group replica")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "print the generated YAML instead of creating the cluster")
	cmd.Flags().BoolVar(&options.wait, "wait", false, "wait for the cluster to be provisioned before returning. Returns an error if the cluster is not provisioned by the timeout specified")
	cmd.Flags().DurationVar(&options.timeout, "timeout", defaultProvisionedTimeout, "the timeout for --wait")

	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *CreateClusterOptions) Complete(cmd *cobra.Command, args []string) error {
	if *options.configFlags.Namespace == "" {
		*options.configFlags.Namespace = "default"
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
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("error retrieving raw config: %w", err)
	}
	if !options.kubeContexter.HasContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
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

func (options *CreateClusterOptions) Run(ctx context.Context, k8sClient client.Client) error {
	if clusterExists(k8sClient.RayClient(), *options.configFlags.Namespace, options.clusterName) {
		return fmt.Errorf("the Ray cluster %s in namespace %s already exists", options.clusterName, *options.configFlags.Namespace)
	}

	rayClusterObject := generation.RayClusterYamlObject{
		Namespace:   *options.configFlags.Namespace,
		ClusterName: options.clusterName,
		RayClusterSpecObject: generation.RayClusterSpecObject{
			RayVersion:     options.rayVersion,
			Image:          options.image,
			HeadCPU:        options.headCPU,
			HeadMemory:     options.headMemory,
			HeadGPU:        options.headGPU,
			WorkerReplicas: options.workerReplicas,
			WorkerCPU:      options.workerCPU,
			WorkerMemory:   options.workerMemory,
			WorkerGPU:      options.workerGPU,
		},
	}

	rayClusterac := rayClusterObject.GenerateRayClusterApplyConfig()

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

	result, err := k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).Apply(ctx, rayClusterac, metav1.ApplyOptions{FieldManager: "kubectl-plugin"})
	if err != nil {
		return fmt.Errorf("failed to create Ray cluster: %w", err)
	}
	fmt.Printf("Created Ray cluster: %s\n", result.GetName())

	if options.wait {
		err = k8sClient.WaitRayClusterProvisioned(ctx, *options.configFlags.Namespace, result.GetName(), options.timeout)
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

package create

import (
	"context"
	"fmt"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

type CreateClusterOptions struct {
	configFlags         *genericclioptions.ConfigFlags
	ioStreams           *genericclioptions.IOStreams
	headNodeSelectors   map[string]string
	workerNodeSelectors map[string]string
	clusterName         string
	rayVersion          string
	image               string
	headCPU             string
	headMemory          string
	headGPU             string
	workerCPU           string
	workerMemory        string
	workerGPU           string
	workerReplicas      int32
	dryRun              bool
}

var (
	createClusterLong = templates.LongDesc(`
		Creates Ray Cluster from inputed file or generate one for user.
	`)

	createClusterExample = templates.Examples(fmt.Sprintf(`
		# Create a Ray Cluster using default values
		kubectl ray create cluster sample-cluster

		# Creates Ray Cluster from flags input
		kubectl ray create cluster sample-cluster --ray-version %s --image %s --head-cpu 1 --head-memory 5Gi --worker-replicas 3 --worker-cpu 1 --worker-memory 5Gi
	`, util.RayVersion, util.RayImage))
)

func NewCreateClusterOptions(streams genericclioptions.IOStreams) *CreateClusterOptions {
	return &CreateClusterOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewCreateClusterCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewCreateClusterOptions(streams)
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "cluster [CLUSTERNAME]",
		Short:        "Create Ray Cluster resource",
		Long:         createClusterLong,
		Example:      createClusterExample,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(cmd, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(cmd.Context(), cmdFactory)
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
	cmd.Flags().StringToStringVar(&options.headNodeSelectors, "head-node-selectors", nil, "Node selectors to apply to all head pods in the cluster (e.g. --head-node-selectors cloud.google.com/gke-accelerator=nvidia-l4,cloud.google.com/gke-nodepool=my-node-pool)")
	cmd.Flags().StringToStringVar(&options.workerNodeSelectors, "worker-node-selectors", nil, "Node selectors to apply to all worker pods in the cluster (e.g. --worker-node-selectors cloud.google.com/gke-accelerator=nvidia-l4,cloud.google.com/gke-nodepool=my-node-pool)")

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
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}

	return nil
}

func (options *CreateClusterOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	k8sClient, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Will generate yaml file
	rayClusterObject := generation.RayClusterYamlObject{
		Namespace:   *options.configFlags.Namespace,
		ClusterName: options.clusterName,
		RayClusterSpecObject: generation.RayClusterSpecObject{
			RayVersion:          options.rayVersion,
			Image:               options.image,
			HeadCPU:             options.headCPU,
			HeadMemory:          options.headMemory,
			HeadGPU:             options.headGPU,
			WorkerReplicas:      options.workerReplicas,
			WorkerCPU:           options.workerCPU,
			WorkerMemory:        options.workerMemory,
			WorkerGPU:           options.workerGPU,
			HeadNodeSelectors:   options.headNodeSelectors,
			WorkerNodeSelectors: options.workerNodeSelectors,
		},
	}

	rayClusterac := rayClusterObject.GenerateRayClusterApplyConfig()

	// If dry run is enabled, it will call the yaml converter and print out the yaml
	if options.dryRun {
		rayClusterYaml, err := generation.ConvertRayClusterApplyConfigToYaml(rayClusterac)
		if err != nil {
			return fmt.Errorf("Error when converting RayClusterApplyConfig to YAML: %w", err)
		}
		fmt.Printf("%s\n", rayClusterYaml)
		return nil
	}

	// TODO: Decide whether to save yaml to file or not.

	// Applying the YAML
	result, err := k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).Apply(ctx, rayClusterac, metav1.ApplyOptions{FieldManager: "kubectl-plugin"})
	if err != nil {
		return fmt.Errorf("Failed to create Ray cluster with: %w", err)
	}
	fmt.Printf("Created Ray Cluster: %s\n", result.GetName())
	return nil
}

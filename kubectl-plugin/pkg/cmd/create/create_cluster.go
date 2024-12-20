package create

import (
	"context"
	"fmt"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

type CreateClusterOptions struct {
	configFlags    *genericclioptions.ConfigFlags
	ioStreams      *genericclioptions.IOStreams
	clusterName    string
	rayVersion     string
	image          string
	headCPU        string
	headMemory     string
	workerGrpName  string
	workerCPU      string
	workerMemory   string
	workerReplicas int32
	dryRun         bool
}

var (
	createClusterLong = templates.LongDesc(`
		Creates Ray Cluster from inputed file or generate one for user.
	`)

	createClusterExample = templates.Examples(`
		# Creates Ray Cluster from flags input
		kubectl ray create cluster sample-cluster --ray-version 2.39.0 --image rayproject/ray:2.39.0 --head-cpu 1 --head-memory 5Gi --worker-grp-name worker-group1 --worker-replicas 3 --worker-cpu 1 --worker-memory 5Gi
	`)
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

	cmd.Flags().StringVar(&options.rayVersion, "ray-version", "2.39.0", "Ray Version to use in the Ray Cluster yaml. Default to 2.39.0")
	cmd.Flags().StringVar(&options.image, "image", options.image, "Ray image to use in the Ray Cluster yaml")
	cmd.Flags().StringVar(&options.headCPU, "head-cpu", "2", "Number of CPU for the ray head. Default to 2")
	cmd.Flags().StringVar(&options.headMemory, "head-memory", "4Gi", "Amount of memory to use for the ray head. Default to 4Gi")
	cmd.Flags().StringVar(&options.workerGrpName, "worker-grp-name", "default-group", "Name of the worker group for the Ray Cluster")
	cmd.Flags().Int32Var(&options.workerReplicas, "worker-replicas", 1, "Number of the worker group replicas. Default of 1")
	cmd.Flags().StringVar(&options.workerCPU, "worker-cpu", "2", "Number of CPU for the ray worker. Default to 2")
	cmd.Flags().StringVar(&options.workerMemory, "worker-memory", "4Gi", "Amount of memory to use for the ray worker. Default to 4Gi")
	cmd.Flags().BoolVar(&options.dryRun, "dry-run", false, "Will not apply the generated cluster and will print out the generated yaml")

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
	if len(config.CurrentContext) == 0 {
		return fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
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
			RayVersion:     options.rayVersion,
			Image:          options.image,
			HeadCPU:        options.headCPU,
			HeadMemory:     options.headMemory,
			WorkerGrpName:  options.workerGrpName,
			WorkerReplicas: options.workerReplicas,
			WorkerCPU:      options.workerCPU,
			WorkerMemory:   options.workerMemory,
		},
	}

	rayClusterac := rayClusterObject.GenerateRayClusterApplyConfig()

	// If dry run is enabled, it will call the yaml converter and print out the yaml
	if options.dryRun {
		rayClusterYaml, err := generation.ConvertRayClusterApplyConfigToYaml(rayClusterac)
		if err != nil {
			return fmt.Errorf("Error when converting RayClusterApplyConfig to yaml: %w", err)
		}
		fmt.Printf("%s\n", rayClusterYaml)
		return nil
	}

	// TODO: Decide whether to save yaml to file or not.

	// Applying the YAML
	result, err := k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).Apply(ctx, rayClusterac, metav1.ApplyOptions{FieldManager: "kubectl-plugin"})
	if err != nil {
		return fmt.Errorf("Failed to create Ray Cluster with: %w", err)
	}
	fmt.Printf("Created Ray Cluster: %s\n", result.GetName())
	return nil
}

package create

import (
	"context"
	"fmt"
	"os"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/generation"
	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/templates"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

type CreateServiceOptions struct {
	configFlags    *genericclioptions.ConfigFlags
	ioStreams      *genericclioptions.IOStreams
	RayService     *rayv1.RayService
	rayServiceName string
	serveConfig    string
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
}

var createServiceExample = templates.Examples(fmt.Sprintf(`
		# Create a Ray service with serve config
		kubectl ray create service rayservice-sample --serve-config path/to/serve-config.yaml

		# Creates Ray service from flags input
		kubectl ray create service rayservice-sample --ray-version %s --image %s --head-cpu 2 --head-memory 2Gi --worker-replicas 1 --worker-cpu 1 --worker-memory 2Gi --serve-config path/to/serve-config.yaml
	`, util.RayVersion, util.RayImage))

func NewCreateServiceOptions(streams genericclioptions.IOStreams) *CreateServiceOptions {
	return &CreateServiceOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewCreateServiceCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewCreateServiceOptions(streams)
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "service [SERVICENAME]",
		Short:        "Create Ray service",
		Example:      createServiceExample,
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

	cmd.Flags().StringVar(&options.serveConfig, "serve-config", options.serveConfig, "Path and name to the serve config file")

	cmd.Flags().StringVar(&options.rayServiceName, "name", "", "Ray service name")
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

	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *CreateServiceOptions) Complete(cmd *cobra.Command, args []string) error {
	if *options.configFlags.Namespace == "" {
		*options.configFlags.Namespace = "default"
	}

	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "%s", cmd.Use)
	}
	options.rayServiceName = args[0]

	if options.image == "" {
		options.image = fmt.Sprintf("rayproject/ray:%s", options.rayVersion)
	}
	return nil
}

func (options *CreateServiceOptions) Validate() error {
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("error retrieving raw config: %w", err)
	}
	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}

	if options.serveConfig == "" {
		return fmt.Errorf("serveConfigV2 is required, use --serveConfig to set the serve config file")
	}

	if len(options.serveConfig) > 0 {
		info, err := os.Stat(options.serveConfig)
		if os.IsNotExist(err) {
			return fmt.Errorf("serve config file does not exist. Failed with: %w", err)
		} else if err != nil {
			return fmt.Errorf("Error occurred when checking serve config file: %w", err)
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("Filename given is not a regular file. Failed with: %w", err)
		}
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

func (options *CreateServiceOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	k8sClients, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to initialize clientset: %w", err)
	}

	// Read the serve config file content
	var serveConfigContent string
	content, err := os.ReadFile(options.serveConfig)
	if err != nil {
		return fmt.Errorf("failed to read serve config file: %w", err)
	}
	serveConfigContent = string(content)

	// Generate the Ray service
	rayServiceObject := generation.RayServiceYamlObject{
		RayServiceName: options.rayServiceName,
		Namespace:      *options.configFlags.Namespace,
		ServeConfig:    serveConfigContent,
		RayClusterSpecObject: generation.RayClusterSpecObject{
			RayVersion:     options.rayVersion,
			Image:          options.image,
			HeadCPU:        options.headCPU,
			HeadMemory:     options.headMemory,
			HeadGPU:        options.headGPU,
			WorkerCPU:      options.workerCPU,
			WorkerMemory:   options.workerMemory,
			WorkerGPU:      options.workerGPU,
			WorkerReplicas: options.workerReplicas,
		},
	}
	rayServiceApplyConfig := rayServiceObject.GenerateRayServiceApplyConfig()

	// If dry run is enabled, it will call the YAML converter and print out the YAML
	if options.dryRun {
		resultYaml, err := generation.ConvertRayServiceApplyConfigToYaml(rayServiceApplyConfig)
		if err != nil {
			return fmt.Errorf("Failed to convert RayService into yaml format: %w", err)
		}

		fmt.Printf("%s\n", resultYaml)
		return nil
	}

	// Apply the generated yaml
	result, err := k8sClients.RayClient().RayV1().RayServices(*options.configFlags.Namespace).Apply(ctx, rayServiceApplyConfig, v1.ApplyOptions{FieldManager: "ray-kubectl-plugin"})
	if err != nil {
		return fmt.Errorf("Failed to apply generated YAML: %w", err)
	}
	fmt.Printf("Created Ray service: %s\n", result.GetName())

	return nil
}

package kubectlraydelete

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
)

type DeleteOptions struct {
	configFlags  *genericclioptions.ConfigFlags
	ioStreams    *genericiooptions.IOStreams
	ResourceType util.ResourceType
	ResourceName string
	Namespace    string
}

var deleteExample = templates.Examples(`
		# Delete Ray cluster
		kubectl ray delete sample-raycluster

		# Delete Ray cluster with specificed Ray resource
		kubectl ray delete raycluster/sample-raycluster

		# Delete RayJob
		kubectl ray delete rayjob/sample-rayjob

		# Delete RayService
		kubectl ray delete rayservice/sample-rayservice
	`)

func NewDeleteOptions(streams genericiooptions.IOStreams) *DeleteOptions {
	configFlags := genericclioptions.NewConfigFlags(true)
	return &DeleteOptions{
		ioStreams:   &streams,
		configFlags: configFlags,
	}
}

func NewDeleteCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewDeleteOptions(streams)
	factory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:               "delete (RAYCLUSTER | TYPE/NAME)",
		Short:             "Delete Ray resource",
		Example:           deleteExample,
		Long:              `Deletes Ray custom resources such as RayCluster, RayService, or RayJob`,
		ValidArgsFunction: completion.RayClusterResourceNameCompletionFunc(factory),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(cmd, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(cmd.Context(), factory)
		},
	}

	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *DeleteOptions) Complete(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "%s", cmd.Use)
	}

	if *options.configFlags.Namespace == "" {
		options.Namespace = "default"
	} else {
		options.Namespace = *options.configFlags.Namespace
	}

	typeAndName := strings.Split(args[0], "/")
	if len(typeAndName) == 1 {
		options.ResourceType = util.RayCluster
		options.ResourceName = typeAndName[0]
	} else {
		if len(typeAndName) != 2 || typeAndName[1] == "" {
			return cmdutil.UsageErrorf(cmd, "invalid resource type/name: %s", args[0])
		}

		switch strings.ToLower(typeAndName[0]) {
		case string(util.RayCluster):
			options.ResourceType = util.RayCluster
		case string(util.RayJob):
			options.ResourceType = util.RayJob
		case string(util.RayService):
			options.ResourceType = util.RayService
		default:
			return cmdutil.UsageErrorf(cmd, "unsupported resource type: %s", args[0])
		}

		options.ResourceName = typeAndName[1]
	}

	return nil
}

func (options *DeleteOptions) Validate() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}
	return nil
}

func (options *DeleteOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	k8sClient, err := client.NewClient(factory)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Ask user for confirmation
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Are you sure you want to delete %s %s? (y/yes/n/no) ", options.ResourceType, options.ResourceName)
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("Failed to read user input: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(confirmation)) {
	case "y", "yes":
	case "n", "no":
		fmt.Printf("Canceled deletion.\n")
		return nil
	default:
		fmt.Printf("Unknown input %s\n", confirmation)
		return nil
	}

	// Delete the Ray Resources
	switch options.ResourceType {
	case util.RayCluster:
		err = k8sClient.RayClient().RayV1().RayClusters(options.Namespace).Delete(ctx, options.ResourceName, metav1.DeleteOptions{})
	case util.RayJob:
		err = k8sClient.RayClient().RayV1().RayJobs(options.Namespace).Delete(ctx, options.ResourceName, metav1.DeleteOptions{})
	case util.RayService:
		err = k8sClient.RayClient().RayV1().RayServices(options.Namespace).Delete(ctx, options.ResourceName, metav1.DeleteOptions{})
	default:
		err = fmt.Errorf("unknown/unsupported resource type: %s", options.ResourceType)
	}

	if err != nil {
		return fmt.Errorf("Failed to delete %s/%s: %w", options.ResourceType, options.ResourceName, err)
	}

	fmt.Printf("Delete %s %s\n", options.ResourceType, options.ResourceName)
	return nil
}

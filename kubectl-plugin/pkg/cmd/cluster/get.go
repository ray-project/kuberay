package cluster

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type ClusterOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	ioStreams     *genericclioptions.IOStreams
	args          []string
	AllNamespaces bool
}

func NewClusterOptions(streams genericclioptions.IOStreams) *ClusterOptions {
	return &ClusterOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewClusterGetCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewClusterOptions(streams)
	// Initialize the factory for later use with the current config flag
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "get [NAME]",
		Short:        "Get cluster information.",
		Aliases:      []string{"list"},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			// running cmd.Execute or cmd.ExecuteE sets the context, which will be done by root
			return options.Run(cmd.Context(), cmdFactory)
		},
	}
	cmd.Flags().BoolVarP(&options.AllNamespaces, "all-namespaces", "A", options.AllNamespaces, "If present, list the requested clusters across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *ClusterOptions) Complete(args []string) error {
	if *options.configFlags.Namespace == "" {
		options.AllNamespaces = true
	}

	options.args = args
	return nil
}

func (options *ClusterOptions) Validate() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if len(config.CurrentContext) == 0 {
		return fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
	}
	if len(options.args) > 1 {
		return fmt.Errorf("too many arguments, either one or no arguments are allowed")
	}
	return nil
}

func (options *ClusterOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	// Retrieves the dynamic client with factory.
	dynamicClient, err := factory.DynamicClient()
	if err != nil {
		return fmt.Errorf("dynamic client failed to initialize: %w", err)
	}

	rayResourceSchema := schema.GroupVersionResource{
		Group:    "ray.io",
		Version:  "v1",
		Resource: "rayclusters",
	}

	var rayclustersList *unstructured.UnstructuredList

	listopts := v1.ListOptions{}
	if len(options.args) == 1 {
		listopts = v1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", options.args[0]),
		}
	}

	if options.AllNamespaces {
		rayclustersList, err = dynamicClient.Resource(rayResourceSchema).List(ctx, listopts)
		if err != nil {
			return fmt.Errorf("unable to retrieve raycluster for all namespaces: %w", err)
		}
	} else {
		rayclustersList, err = dynamicClient.Resource(rayResourceSchema).Namespace(*options.configFlags.Namespace).List(ctx, listopts)
		if err != nil {
			return fmt.Errorf("unable to retrieve raycluster for namespace %s: %w", *options.configFlags.Namespace, err)
		}
	}

	return printClusters(rayclustersList, options.ioStreams.Out)
}

func printClusters(rayclustersList *unstructured.UnstructuredList, output io.Writer) error {
	resultTablePrinter := printers.NewTablePrinter(printers.PrintOptions{})

	resTable := &v1.Table{
		ColumnDefinitions: []v1.TableColumnDefinition{
			{Name: "Name", Type: "string"},
			{Name: "Namespace", Type: "string"},
			{Name: "Desired Workers", Type: "string"},
			{Name: "Available Workers", Type: "string"},
			{Name: "CPUs", Type: "string"},
			{Name: "GPUs", Type: "string"},
			{Name: "TPUs", Type: "string"},
			{Name: "Memory", Type: "string"},
			{Name: "Age", Type: "string"},
		},
	}

	for _, raycluster := range rayclustersList.Items {
		age := duration.HumanDuration(time.Since(raycluster.GetCreationTimestamp().Time))
		if raycluster.GetCreationTimestamp().Time.IsZero() {
			age = "<unknown>"
		}
		resTable.Rows = append(resTable.Rows, v1.TableRow{
			Cells: []interface{}{
				raycluster.GetName(),
				raycluster.GetNamespace(),
				raycluster.Object["status"].(map[string]interface{})["desiredWorkerReplicas"],
				raycluster.Object["status"].(map[string]interface{})["availableWorkerReplicas"],
				raycluster.Object["status"].(map[string]interface{})["desiredCPU"],
				raycluster.Object["status"].(map[string]interface{})["desiredGPU"],
				raycluster.Object["status"].(map[string]interface{})["desiredTPU"],
				raycluster.Object["status"].(map[string]interface{})["desiredMemory"],
				age,
			},
		})
	}

	return resultTablePrinter.PrintObj(resTable, output)
}

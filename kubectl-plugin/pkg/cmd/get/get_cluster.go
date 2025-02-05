package get

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

type GetClusterOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	ioStreams     *genericclioptions.IOStreams
	args          []string
	AllNamespaces bool
}

func NewGetClusterOptions(streams genericclioptions.IOStreams) *GetClusterOptions {
	return &GetClusterOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewGetClusterCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewGetClusterOptions(streams)
	// Initialize the factory for later use with the current config flag
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:               "cluster [NAME]",
		Short:             "Get cluster information.",
		SilenceUsage:      true,
		ValidArgsFunction: completion.RayClusterCompletionFunc(cmdFactory),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			// running cmd.Execute or cmd.ExecuteE sets the context, which will be done by root
			k8sClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			return options.Run(cmd.Context(), k8sClient)
		},
	}
	cmd.Flags().BoolVarP(&options.AllNamespaces, "all-namespaces", "A", options.AllNamespaces, "If present, list the requested clusters across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *GetClusterOptions) Complete(args []string) error {
	if *options.configFlags.Namespace == "" {
		options.AllNamespaces = true
	}

	options.args = args
	return nil
}

func (options *GetClusterOptions) Validate() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if !util.HasKubectlContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}
	if len(options.args) > 1 {
		return fmt.Errorf("too many arguments, either one or no arguments are allowed")
	}
	return nil
}

func (options *GetClusterOptions) Run(ctx context.Context, k8sClient client.Client) error {
	var err error
	var rayclusterList *rayv1.RayClusterList

	listopts := v1.ListOptions{}
	if len(options.args) == 1 {
		listopts = v1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", options.args[0]),
		}
	}

	if options.AllNamespaces {
		rayclusterList, err = k8sClient.RayClient().RayV1().RayClusters("").List(ctx, listopts)
		if err != nil {
			return fmt.Errorf("unable to retrieve raycluster for all namespaces: %w", err)
		}
	} else {
		rayclusterList, err = k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).List(ctx, listopts)
		if err != nil {
			return fmt.Errorf("unable to retrieve raycluster for namespace %s: %w", *options.configFlags.Namespace, err)
		}
	}

	return printClusters(rayclusterList, options.ioStreams.Out)
}

func printClusters(rayclusterList *rayv1.RayClusterList, output io.Writer) error {
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

	for _, raycluster := range rayclusterList.Items {
		age := duration.HumanDuration(time.Since(raycluster.GetCreationTimestamp().Time))
		if raycluster.GetCreationTimestamp().Time.IsZero() {
			age = "<unknown>"
		}
		resTable.Rows = append(resTable.Rows, v1.TableRow{
			Cells: []interface{}{
				raycluster.GetName(),
				raycluster.GetNamespace(),
				raycluster.Status.DesiredWorkerReplicas,
				raycluster.Status.AvailableWorkerReplicas,
				raycluster.Status.DesiredCPU.String(),
				raycluster.Status.DesiredGPU.String(),
				raycluster.Status.DesiredTPU.String(),
				raycluster.Status.DesiredMemory.String(),
				age,
			},
		})
	}

	return resultTablePrinter.PrintObj(resTable, output)
}

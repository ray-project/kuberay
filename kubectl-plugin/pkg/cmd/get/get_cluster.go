package get

import (
	"context"
	"errors"
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
	cluster       string
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
		Aliases:           []string{"clusters"},
		Short:             "Get cluster information.",
		SilenceUsage:      true,
		ValidArgsFunction: completion.RayClusterCompletionFunc(cmdFactory),
		Args:              cobra.MaximumNArgs(1),
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

	if len(args) >= 1 {
		options.cluster = args[0]
	}

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
	return nil
}

func (options *GetClusterOptions) Run(ctx context.Context, k8sClient client.Client) error {
	rayclusterList, err := getRayClusters(ctx, options, k8sClient)
	if err != nil {
		return err
	}

	return printClusters(rayclusterList, options.ioStreams.Out)
}

func getRayClusters(ctx context.Context, options *GetClusterOptions, k8sClient client.Client) (*rayv1.RayClusterList, error) {
	var rayclusterList *rayv1.RayClusterList
	var err error

	listopts := v1.ListOptions{}
	if options.cluster != "" {
		listopts = v1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", options.cluster),
		}
	}

	if options.AllNamespaces {
		rayclusterList, err = k8sClient.RayClient().RayV1().RayClusters("").List(ctx, listopts)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve Ray clusters for all namespaces: %w", err)
		}
	} else {
		rayclusterList, err = k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).List(ctx, listopts)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve Ray clusters for namespace %s: %w", *options.configFlags.Namespace, err)
		}
	}

	if options.cluster != "" && len(rayclusterList.Items) == 0 {
		errMsg := fmt.Sprintf("Ray cluster %s not found", options.cluster)
		if options.AllNamespaces {
			errMsg += " in any namespace"
		} else {
			errMsg += fmt.Sprintf(" in namespace %s", *options.configFlags.Namespace)
		}
		return nil, errors.New(errMsg)
	}

	return rayclusterList, nil
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
			{Name: "Condition", Type: "string"},
			{Name: "Status", Type: "string"},
			{Name: "Age", Type: "string"},
		},
	}

	for _, raycluster := range rayclusterList.Items {
		age := duration.HumanDuration(time.Since(raycluster.GetCreationTimestamp().Time))
		if raycluster.GetCreationTimestamp().Time.IsZero() {
			age = "<unknown>"
		}
		relevantConditionType := ""
		relevantCondition := util.RelevantRayClusterCondition(raycluster)
		if relevantCondition != nil {
			relevantConditionType = relevantCondition.Type
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
				relevantConditionType,
				raycluster.Status.State, //nolint:staticcheck // Display State for now until it's removed from the CRD
				age,
			},
		})
	}

	return resultTablePrinter.PrintObj(resTable, output)
}

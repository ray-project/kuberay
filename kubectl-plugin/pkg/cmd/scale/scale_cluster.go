package scale

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
)

type ScaleClusterOptions struct {
	cmdFactory  cmdutil.Factory
	ioStreams   *genericclioptions.IOStreams
	replicas    *int32
	namespace   string
	workerGroup string
	cluster     string
}

var (
	scaleLong = templates.LongDesc(`
		Scale a Ray cluster by worker group
	`)

	scaleExample = templates.Examples(`
		# Scale a Ray cluster by setting one of its worker groups to 3 replicas
		kubectl ray scale cluster my-cluster --worker-group my-group --replicas 3
	`)
)

func NewScaleClusterOptions(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *ScaleClusterOptions {
	return &ScaleClusterOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
		replicas:   new(int32),
	}
}

func NewScaleClusterCommand(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewScaleClusterOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:          "cluster (RAYCLUSTER) (-w/--worker-group WORKERGROUP) (-r/--replicas N)",
		Short:        "Scale a Ray cluster",
		Long:         scaleLong,
		Example:      scaleExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args, cmd); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			k8sClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			return options.Run(cmd.Context(), k8sClient, os.Stdout)
		},
	}

	cmd.Flags().StringVarP(&options.workerGroup, "worker-group", "w", "", "worker group")
	cobra.CheckErr(cmd.MarkFlagRequired("worker-group"))
	cmd.Flags().Int32VarP(options.replicas, "replicas", "r", -1, "Desired number of replicas in worker group")
	return cmd
}

func (options *ScaleClusterOptions) Complete(args []string, cmd *cobra.Command) error {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}
	options.namespace = namespace
	if options.namespace == "" {
		options.namespace = "default"
	}

	options.cluster = args[0]

	return nil
}

func (options *ScaleClusterOptions) Validate() error {
	if options.workerGroup == "" {
		return fmt.Errorf("must specify -w/--worker-group")
	}

	if options.replicas == nil || *options.replicas < 0 {
		return fmt.Errorf("must specify -r/--replicas with a non-negative integer")
	}

	return nil
}

func (options *ScaleClusterOptions) Run(ctx context.Context, k8sClient client.Client, writer io.Writer) error {
	cluster, err := k8sClient.RayClient().RayV1().RayClusters(options.namespace).Get(ctx, options.cluster, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale worker group %s in Ray cluster %s in namespace %s: %w", options.workerGroup, options.cluster, options.namespace, err)
	}

	// find the index of the worker group
	var workerGroups []string
	workerGroupIndex := -1
	for i, workerGroup := range cluster.Spec.WorkerGroupSpecs {
		workerGroups = append(workerGroups, workerGroup.GroupName)
		if workerGroup.GroupName == options.workerGroup {
			workerGroupIndex = i
		}
	}
	if workerGroupIndex == -1 {
		return fmt.Errorf("worker group %s not found in Ray cluster %s in namespace %s. Available worker groups: %s", options.workerGroup, options.cluster, options.namespace, strings.Join(workerGroups, ", "))
	}

	previousReplicas := *cluster.Spec.WorkerGroupSpecs[workerGroupIndex].Replicas
	if previousReplicas == *options.replicas {
		fmt.Fprintf(writer, "worker group %s in Ray cluster %s in namespace %s already has %d replicas. Skipping\n", options.workerGroup, options.cluster, options.namespace, previousReplicas)
		return nil
	}

	cluster.Spec.WorkerGroupSpecs[workerGroupIndex].Replicas = options.replicas
	_, err = k8sClient.RayClient().RayV1().RayClusters(options.namespace).Update(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale worker group %s in Ray cluster %s in namespace %s: %w", options.workerGroup, options.cluster, options.namespace, err)
	}

	fmt.Fprintf(writer, "Scaled worker group %s in Ray cluster %s in namespace %s from %d to %d replicas\n", options.workerGroup, options.cluster, options.namespace, previousReplicas, *options.replicas)
	return nil
}

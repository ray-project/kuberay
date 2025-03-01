package scale

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/templates"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type ScaleClusterOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	ioStreams     *genericclioptions.IOStreams
	kubeContexter util.KubeContexter
	replicas      *int32
	workerGroup   string
	cluster       string
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

func NewScaleClusterOptions(streams genericclioptions.IOStreams) *ScaleClusterOptions {
	return &ScaleClusterOptions{
		configFlags:   genericclioptions.NewConfigFlags(true),
		ioStreams:     &streams,
		kubeContexter: &util.DefaultKubeContexter{},
		replicas:      new(int32),
	}
}

func NewScaleClusterCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewScaleClusterOptions(streams)
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "cluster (RAYCLUSTER) (-w/--worker-group WORKERGROUP) (-r/--replicas N)",
		Short:        "Scale a Ray cluster",
		Long:         scaleLong,
		Example:      scaleExample,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args); err != nil {
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
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *ScaleClusterOptions) Complete(args []string) error {
	if *options.configFlags.Namespace == "" {
		*options.configFlags.Namespace = "default"
	}

	options.cluster = args[0]

	return nil
}

func (options *ScaleClusterOptions) Validate() error {
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("error retrieving raw config: %w", err)
	}

	if !options.kubeContexter.HasContext(config, options.configFlags) {
		return fmt.Errorf("no context is currently set, use %q or %q to select a new one", "--context", "kubectl config use-context <context>")
	}

	if options.workerGroup == "" {
		return fmt.Errorf("must specify -w/--worker-group")
	}

	if options.replicas == nil || *options.replicas < 0 {
		return fmt.Errorf("must specify -r/--replicas with a non-negative integer")
	}

	return nil
}

func (options *ScaleClusterOptions) Run(ctx context.Context, k8sClient client.Client, writer io.Writer) error {
	cluster, err := k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).Get(ctx, options.cluster, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale worker group %s in Ray cluster %s in namespace %s: %w", options.workerGroup, options.cluster, *options.configFlags.Namespace, err)
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
		return fmt.Errorf("worker group %s not found in Ray cluster %s in namespace %s. Available worker groups: %s", options.workerGroup, options.cluster, *options.configFlags.Namespace, strings.Join(workerGroups, ", "))
	}

	previousReplicas := *cluster.Spec.WorkerGroupSpecs[workerGroupIndex].Replicas
	if previousReplicas == *options.replicas {
		fmt.Fprintf(writer, "worker group %s in Ray cluster %s in namespace %s already has %d replicas. Skipping\n", options.workerGroup, options.cluster, *options.configFlags.Namespace, previousReplicas)
		return nil
	}

	cluster.Spec.WorkerGroupSpecs[workerGroupIndex].Replicas = options.replicas
	_, err = k8sClient.RayClient().RayV1().RayClusters(*options.configFlags.Namespace).Update(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale worker group %s in Ray cluster %s in namespace %s: %w", options.workerGroup, options.cluster, *options.configFlags.Namespace, err)
	}

	fmt.Fprintf(writer, "Scaled worker group %s in Ray cluster %s in namespace %s from %d to %d replicas\n", options.workerGroup, options.cluster, *options.configFlags.Namespace, previousReplicas, *options.replicas)
	return nil
}

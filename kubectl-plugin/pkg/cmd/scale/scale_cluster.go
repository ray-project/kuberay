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
	minReplicas *int32
	maxReplicas *int32
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
		cmdFactory:  cmdFactory,
		ioStreams:   &streams,
		replicas:    new(int32),
		minReplicas: new(int32),
		maxReplicas: new(int32),
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
	cmd.Flags().Int32VarP(options.minReplicas, "min-replicas", "", -1, "Minimum number of replicas for worker group (optional)")
	cmd.Flags().Int32VarP(options.maxReplicas, "max-replicas", "", -1, "Maximum number of replicas for worker group (optional)")
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

	minSet := options.minReplicas != nil && *options.minReplicas > -1
	maxSet := options.maxReplicas != nil && *options.maxReplicas > -1
	desiredSet := options.replicas != nil && *options.replicas > -1

	// at least one of --replicas, --min-replicas, or --max-replicas flags must be set
	if !minSet && !maxSet && !desiredSet {
		return fmt.Errorf("must specify at least one non negative --replicas, --min-replicas, or --max-replicas")
	}

	if desiredSet && *options.replicas < 0 {
		return fmt.Errorf("must specify -r/--replicas with a non-negative integer")
	}

	if minSet {
		if *options.minReplicas < 0 {
			return fmt.Errorf("minimum replicas (--min-replicas) must be a non-negative integer")
		}
	}
	if maxSet {
		if *options.maxReplicas < 0 {
			return fmt.Errorf("maximum replicas (--max-replicas) must be a non-negative integer")
		}
	}

	if minSet && maxSet {
		if *options.minReplicas > *options.maxReplicas {
			return fmt.Errorf("minimum replicas (%d) cannot be greater than maximum replicas (%d)", *options.minReplicas, *options.maxReplicas)
		}
	}

	// If desired is set, ensure it respects min/max if they are also set
	if desiredSet {
		if minSet && *options.replicas < *options.minReplicas {
			return fmt.Errorf("desired replicas (%d) cannot be less than minimum replicas (%d)", *options.replicas, *options.minReplicas)
		}
		if maxSet && *options.replicas > *options.maxReplicas {
			return fmt.Errorf("desired replicas (%d) cannot be greater than maximum replicas (%d)", *options.replicas, *options.maxReplicas)
		}
	}

	return nil
}

func (options *ScaleClusterOptions) Run(ctx context.Context, k8sClient client.Client, writer io.Writer) error {
	cluster, err := k8sClient.RayClient().RayV1().RayClusters(options.namespace).Get(ctx, options.cluster, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Ray cluster %s in namespace %s: %w", options.cluster, options.namespace, err)
	}

	// find the index of the worker group
	var workerGroups []string
	workerGroupIndex := -1
	for i, workerGroup := range cluster.Spec.WorkerGroupSpecs {
		workerGroups = append(workerGroups, workerGroup.GroupName)
		if workerGroup.GroupName == options.workerGroup {
			workerGroupIndex = i
			break
		}
	}
	if workerGroupIndex == -1 {
		return fmt.Errorf("worker group %s not found in Ray cluster %s in namespace %s. Available worker groups: %s", options.workerGroup, options.cluster, options.namespace, strings.Join(workerGroups, ", "))
	}

	targetWorkerGroupSpec := &cluster.Spec.WorkerGroupSpecs[workerGroupIndex]
	var changes []string

	// sets initial nil states for consistency check
	initialMinReplicasNil := targetWorkerGroupSpec.MinReplicas == nil
	initialMaxReplicasNil := targetWorkerGroupSpec.MaxReplicas == nil

	isMinOptionProvided := options.minReplicas != nil && *options.minReplicas >= 0
	isMaxOptionProvided := options.maxReplicas != nil && *options.maxReplicas >= 0

	// validate that if one of minReplicas or maxReplicas is being set from a nil state,
	// prevents inconsistent states like (min=2, max=nil) when max was also nil initially.
	if (initialMinReplicasNil && isMinOptionProvided && initialMaxReplicasNil && !isMaxOptionProvided) ||
		(initialMaxReplicasNil && isMaxOptionProvided && initialMinReplicasNil && !isMinOptionProvided) {
		return fmt.Errorf("cannot set only one of minReplicas or maxReplicas when both are currently unset (nil) in worker group %s. Please specify both or neither for a valid range", options.workerGroup)
	}

	originalMinReplicas := targetWorkerGroupSpec.MinReplicas
	originalMaxReplicas := targetWorkerGroupSpec.MaxReplicas
	originalReplicas := targetWorkerGroupSpec.Replicas

	if isMinOptionProvided {
		previousMinReplicas := int32(0)
		if targetWorkerGroupSpec.MinReplicas != nil {
			previousMinReplicas = *targetWorkerGroupSpec.MinReplicas
		}

		if previousMinReplicas != *options.minReplicas {
			targetWorkerGroupSpec.MinReplicas = options.minReplicas
			changes = append(changes, fmt.Sprintf("minReplicas from %d to %d", previousMinReplicas, *options.minReplicas))
		}
	}

	if isMaxOptionProvided {
		previousMaxReplicas := int32(0)
		if targetWorkerGroupSpec.MaxReplicas != nil {
			previousMaxReplicas = *targetWorkerGroupSpec.MaxReplicas
		}

		if previousMaxReplicas != *options.maxReplicas {
			targetWorkerGroupSpec.MaxReplicas = options.maxReplicas
			changes = append(changes, fmt.Sprintf("maxReplicas from %d to %d", previousMaxReplicas, *options.maxReplicas))
		}
	}

	// TargetWorkerGroupSpec.MinReplicas and .MaxReplicas have the *proposed* new values or are the original values if not provided

	if targetWorkerGroupSpec.MinReplicas != nil && targetWorkerGroupSpec.MaxReplicas != nil {
		if *targetWorkerGroupSpec.MinReplicas > *targetWorkerGroupSpec.MaxReplicas {
			targetWorkerGroupSpec.MinReplicas = originalMinReplicas
			targetWorkerGroupSpec.MaxReplicas = originalMaxReplicas
			return fmt.Errorf("proposed minReplicas (%d) cannot be greater than proposed maxReplicas (%d) for worker group %s", *options.minReplicas, *options.maxReplicas, options.workerGroup)
		}
	}

	if options.replicas != nil && *options.replicas >= 0 {
		proposedReplicas := *options.replicas

		if targetWorkerGroupSpec.MinReplicas != nil && proposedReplicas < *targetWorkerGroupSpec.MinReplicas {
			return fmt.Errorf("proposed replicas (%d) cannot be less than the current or proposed minReplicas (%d) for worker group %s", proposedReplicas, *targetWorkerGroupSpec.MinReplicas, options.workerGroup)
		}

		if targetWorkerGroupSpec.MaxReplicas != nil && proposedReplicas > *targetWorkerGroupSpec.MaxReplicas {
			return fmt.Errorf("proposed replicas (%d) cannot be greater than the current or proposed maxReplicas (%d) for worker group %s", proposedReplicas, *targetWorkerGroupSpec.MaxReplicas, options.workerGroup)
		}

		previousReplicas := int32(0)
		if originalReplicas != nil {
			previousReplicas = *originalReplicas
		}

		if previousReplicas != proposedReplicas {
			targetWorkerGroupSpec.Replicas = options.replicas
			changes = append(changes, fmt.Sprintf("replicas from %d to %d", previousReplicas, proposedReplicas))
		}
	}

	if len(changes) == 0 {
		fmt.Fprintf(writer, "No changes needed for worker group %s in Ray cluster %s in namespace %s. All specified values already match.", options.workerGroup, options.cluster, options.namespace)
		return nil
	}

	_, err = k8sClient.RayClient().RayV1().RayClusters(options.namespace).Update(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update Ray cluster %s in namespace %s: %w", options.cluster, options.namespace, err)
	}

	fmt.Fprintf(writer, "Updated worker group %s in Ray cluster %s in namespace %s: %s",
		options.workerGroup, options.cluster, options.namespace, strings.Join(changes, ", "))
	return nil
}

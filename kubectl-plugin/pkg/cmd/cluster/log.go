package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type ClusterLogOptions struct {
	configFlags *genericclioptions.ConfigFlags
	ioStreams   *genericclioptions.IOStreams
	outputDir   string
	args        []string
	downloadLog bool
}

func NewClusterLogOptions(streams genericclioptions.IOStreams) *ClusterLogOptions {
	return &ClusterLogOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
	}
}

func NewClusterLogCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewClusterLogOptions(streams)
	// Initialize the factory for later use with the current config flag
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "log [CLUSTER_NAME]",
		Short:        "Get cluster log",
		Aliases:      []string{"logs"},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(cmd.Context(), cmdFactory)
		},
	}
	cmd.Flags().StringVar(&options.outputDir, "out-dir", options.outputDir, "File Directory PATH of where to download the file logs to.")
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *ClusterLogOptions) Complete(args []string) error {
	if options.outputDir != "" {
		options.downloadLog = true
	}

	options.args = args
	return nil
}

func (options *ClusterLogOptions) Validate() error {
	// Overrides and binds the kube config then retrieves the merged result
	config, err := options.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("Error retrieving raw config: %w", err)
	}
	if len(config.CurrentContext) == 0 {
		return fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
	}
	if options.downloadLog {
		info, err := os.Stat(options.outputDir)
		if os.IsNotExist(err) {
			return fmt.Errorf("Directory does not exist. Failed with %w", err)
		} else if err != nil {
			return fmt.Errorf("Error occurred will checking directory %w", err)
		} else if !info.IsDir() {
			return fmt.Errorf("Path is Not a directory. Please input a directory and try again")
		}
	}
	if len(options.args) != 1 {
		return fmt.Errorf("must have one argument")
	}
	return nil
}

func (options *ClusterLogOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	kubeClientSet, err := factory.KubernetesClientSet()
	if err != nil {
		return fmt.Errorf("failed to retrieve kubernetes client set: %w", err)
	}

	listopts := v1.ListOptions{
		LabelSelector: fmt.Sprintf("ray.io/group=headgroup, ray.io/cluster=%s", options.args[0]),
	}

	// Get list of nodes that are considered ray heads
	rayHeads, err := kubeClientSet.CoreV1().Pods(*options.configFlags.Namespace).List(ctx, listopts)
	if err != nil {
		return fmt.Errorf("failed to retrieve head node for cluster %s: %w", options.args[0], err)
	}

	var logList []*bytes.Buffer
	for _, rayHead := range rayHeads.Items {
		request := kubeClientSet.CoreV1().Pods(rayHead.Namespace).GetLogs(rayHead.Name, &corev1.PodLogOptions{})

		podLogs, err := request.Stream(ctx)
		if err != nil {
			return fmt.Errorf("Error retrieving log for kuberay-head %s: %w", rayHead.Name, err)
		}
		defer podLogs.Close()

		// Get current logs:
		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			return fmt.Errorf("Failed to get read current logs for kuberay-head %s: %w", rayHead.Name, err)
		}

		logList = append(logList, buf)
	}

	// depending on the if there is a out-dir or not, it will either print out the log or download the logs
	// File name format is name of the ray head
	if options.downloadLog {
		for ind, logList := range logList {
			curFilePath := filepath.Join(options.outputDir, rayHeads.Items[ind].Name)
			file, err := os.OpenFile(curFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("failed to create/open file for kuberay-head with path: %s: %w", rayHeads.Items[ind].Name, err)
			}
			defer file.Close()

			_, err = logList.WriteTo(file)
			if err != nil {
				return fmt.Errorf("failed to write to file for kuberay-head: %s: %w", rayHeads.Items[ind].Name, err)
			}
		}
	} else {
		for ind, logList := range logList {
			fmt.Fprintf(options.ioStreams.Out, "Head Name: %s\n", rayHeads.Items[ind].Name)
			fmt.Fprint(options.ioStreams.Out, logList.String())
		}
	}
	return nil
}

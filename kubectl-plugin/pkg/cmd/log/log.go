package log

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

const filePathInPod = "/tmp/ray/session_latest/logs/"

type ClusterLogOptions struct {
	configFlags *genericclioptions.ConfigFlags
	ioStreams   *genericclioptions.IOStreams
	Executor    RemoteExecutor
	outputDir   string
	nodeType    string
	args        []string
}

func NewClusterLogOptions(streams genericclioptions.IOStreams) *ClusterLogOptions {
	return &ClusterLogOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		ioStreams:   &streams,
		Executor:    &DefaultRemoteExecutor{},
	}
}

func NewClusterLogCommand(streams genericclioptions.IOStreams) *cobra.Command {
	options := NewClusterLogOptions(streams)
	// Initialize the factory for later use with the current config flag
	cmdFactory := cmdutil.NewFactory(options.configFlags)

	cmd := &cobra.Command{
		Use:          "log (RAY_CLUSTER_NAME) [--out-dir DIR_PATH] [--node-type all|head|worker]",
		Short:        "Get ray cluster log",
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
	cmd.Flags().StringVar(&options.nodeType, "node-type", options.nodeType, "Type of Ray node to download the files for.")
	options.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (options *ClusterLogOptions) Complete(args []string) error {
	options.args = args

	if options.nodeType == "" {
		options.nodeType = "head"
	}

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

	// Command must have ray cluster name
	if len(options.args) != 1 {
		return fmt.Errorf("must have at only one argument")
	} else if options.outputDir == "" {
		fmt.Fprintln(options.ioStreams.Out, "No output directory specified, creating dir under current directory using cluster name.")
		options.outputDir = options.args[0]
		err := os.MkdirAll(options.outputDir, 0o755)
		if err != nil {
			return fmt.Errorf("could not create directory with cluster name %s: %w", options.outputDir, err)
		}
	}

	switch options.nodeType {
	case "all":
		return fmt.Errorf("node type `all` is currently not supported")
	case "head":
		break
	case "worker":
		return fmt.Errorf("node type `worker` is currently not supported")
	default:
		return fmt.Errorf("unknown node type `%s`", options.nodeType)
	}

	info, err := os.Stat(options.outputDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("Directory does not exist. Failed with: %w", err)
	} else if err != nil {
		return fmt.Errorf("Error occurred will checking directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("Path is Not a directory. Please input a directory and try again")
	}

	return nil
}

func (options *ClusterLogOptions) Run(ctx context.Context, factory cmdutil.Factory) error {
	kubeClientSet, err := factory.KubernetesClientSet()
	if err != nil {
		return fmt.Errorf("failed to retrieve kubernetes client set: %w", err)
	}

	var listopts v1.ListOptions
	if options.nodeType == "head" {
		listopts = v1.ListOptions{
			LabelSelector: fmt.Sprintf("ray.io/group=headgroup, ray.io/cluster=%s", options.args[0]),
		}
	}

	// Get list of nodes that are considered ray heads
	rayHeads, err := kubeClientSet.CoreV1().Pods(*options.configFlags.Namespace).List(ctx, listopts)
	if err != nil {
		return fmt.Errorf("failed to retrieve head node for cluster %s: %w", options.args[0], err)
	}

	// Get a list of logs of the ray heads.
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

	// Pod file name format is name of the ray head
	for ind, logList := range logList {
		curFilePath := filepath.Join(options.outputDir, rayHeads.Items[ind].Name, "stdout.log")
		dirPath := filepath.Join(options.outputDir, rayHeads.Items[ind].Name)
		err := os.MkdirAll(dirPath, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create directory within path %s: %w", dirPath, err)
		}
		file, err := os.OpenFile(curFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("failed to create/open file for kuberay-head with path: %s: %w", curFilePath, err)
		}
		defer file.Close()

		_, err = logList.WriteTo(file)
		if err != nil {
			return fmt.Errorf("failed to write to file for kuberay-head: %s: %w", rayHeads.Items[ind].Name, err)
		}

		req := kubeClientSet.CoreV1().RESTClient().
			Get().
			Namespace(rayHeads.Items[ind].Namespace).
			Resource("pods").
			Name(rayHeads.Items[ind].Name).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Command: []string{"tar", "--warning=no-file-changed", "-cf", "-", "-C", filePathInPod, "."},
				Stdin:   true,
				Stdout:  true,
				Stderr:  true,
				TTY:     false,
			}, clientgoscheme.ParameterCodec)

		restconfig, err := factory.ToRESTConfig()
		if err != nil {
			return fmt.Errorf("failed to get restconfig: %w", err)
		}

		exec, err := options.Executor.CreateExecutor(restconfig, req.URL())
		if err != nil {
			return fmt.Errorf("failed to create executor with error: %w", err)
		}

		err = options.downloadRayLogFiles(ctx, exec, rayHeads.Items[ind])
		if err != nil {
			return fmt.Errorf("failed to download ray head log files with error: %w", err)
		}
	}
	return nil
}

// RemoteExecutor creates the executor for executing exec on the pod - provided for testing purposes
type RemoteExecutor interface {
	CreateExecutor(restConfig *rest.Config, url *url.URL) (remotecommand.Executor, error)
}

type DefaultRemoteExecutor struct{}

// CreateExecutor returns the executor created by NewSPDYExecutor
func (dre *DefaultRemoteExecutor) CreateExecutor(restConfig *rest.Config, url *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewSPDYExecutor(restConfig, "POST", url)
}

// downloadRayLogFiles will use to the executor and retrieve the logs file from the inputted ray head
func (options *ClusterLogOptions) downloadRayLogFiles(ctx context.Context, exec remotecommand.Executor, rayhead corev1.Pod) error {
	outreader, outStream := io.Pipe()
	go func() {
		defer outStream.Close()
		err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  options.ioStreams.In,
			Stdout: outStream,
			Stderr: options.ioStreams.ErrOut,
			Tty:    false,
		})
		if err != nil {
			log.Fatalf("Error occurred while calling remote command: %v", err)
		}
	}()

	// Goes through the tar and create/copy them one by one into the destination dir
	tarReader := tar.NewReader(outreader)
	header, err := tarReader.Next()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("error will extracting head tar file for ray head %s: %w", rayhead.Name, err)
	}
	for !errors.Is(err, io.EOF) {
		fmt.Printf("Downloading file %s for Ray Head %s\n", header.Name, rayhead.Name)
		if err != nil {
			return fmt.Errorf("Error reading tar archive: %w", err)
		}

		// Construct the full local path and a directory for the tmp file logs
		localFilePath := filepath.Join(path.Clean(options.outputDir), path.Clean(rayhead.Name), path.Clean(header.Name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(localFilePath, 0o755); err != nil {
				return fmt.Errorf("Error creating directory: %w", err)
			}
		case tar.TypeReg:
			// Check for overflow: G115
			if header.Mode < 0 || header.Mode > math.MaxUint32 {
				fmt.Fprintf(options.ioStreams.Out, "file mode out side of accceptable value %d skipping file", header.Mode)
			}
			// Create file and write contents
			outFile, err := os.OpenFile(localFilePath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode)) //nolint:gosec // lint failing due to file mode conversion from uint64 to int32, checked above
			if err != nil {
				return fmt.Errorf("Error creating file: %w", err)
			}
			defer outFile.Close()
			// This is to limit the copy size for a decompression bomb, currently set arbitrarily
			for {
				n, err := io.CopyN(outFile, tarReader, 1000000)
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return fmt.Errorf("failed while writing to file: %w", err)
				}
				if n == 0 {
					break
				}
			}
		default:
			fmt.Printf("Ignoring unsupported file type: %b", header.Typeflag)
		}

		header, err = tarReader.Next()
		if header == nil && err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("error while extracting tar file with error: %w", err)
		}
	}

	return nil
}

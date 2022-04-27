package compute

import (
	"context"
	"log"
	"time"

	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type CreateOptions struct {
	name           string
	namespace      string
	cpu            uint32
	memory         uint32
	gpu            uint32
	gpuAccelerator string
}

func newCmdCreate() *cobra.Command {
	opts := CreateOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a compute template",
		Long:  "Currently only one worker group is supported in CLI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return createComputeTemplate(opts)
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "name of the compute template")
	cmd.Flags().StringVar(&opts.namespace, "namespace", "ray-system", "kubernetes namespace where the compute template will be")
	cmd.Flags().Uint32Var(&opts.cpu, "cpu", 1, "ray pod CPU")
	cmd.Flags().Uint32Var(&opts.memory, "memory", 1, "ray pod memory in GB")
	cmd.Flags().Uint32Var(&opts.gpu, "gpu", 0, "ray head GPU")
	cmd.Flags().StringVar(&opts.gpuAccelerator, "gpu-accelerator", "", "GPU Accelerator type")
	if err := cmd.MarkFlagRequired("name"); err != nil {
		klog.Warning(err)
	}

	return cmd
}

func createComputeTemplate(opts CreateOptions) error {
	conn, err := cmdutil.GetGrpcConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	// build gRPC client
	client := go_client.NewComputeTemplateServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	computeTemplate := &go_client.ComputeTemplate{
		Name:           opts.name,
		Namespace:      opts.namespace,
		Cpu:            opts.cpu,
		Memory:         opts.memory,
		Gpu:            opts.gpu,
		GpuAccelerator: opts.gpuAccelerator,
	}

	r, err := client.CreateComputeTemplate(ctx, &go_client.CreateComputeTemplateRequest{
		ComputeTemplate: computeTemplate,
	})
	if err != nil {
		log.Fatalf("could not create compute template %v", err)
	}

	log.Printf("compute template %v is created in %v", r.Name, r.Namespace)
	return nil
}

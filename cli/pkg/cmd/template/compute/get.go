package compute

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type GetOptions struct {
	namespace string
}

func newCmdGet() *cobra.Command {
	opts := GetOptions{}

	cmd := &cobra.Command{
		Use:   "get <compute template name>",
		Short: "Get a compute template by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getComputeTemplate(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "",
		"kubernetes namespace where the compute template is stored")
	if err := cmd.MarkFlagRequired("namespace"); err != nil {
		klog.Warning(err)
	}

	return cmd
}

func getComputeTemplate(name string, opts GetOptions) error {
	// Get gRPC connection
	conn, err := cmdutil.GetGrpcConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	// build gRPC client
	client := go_client.NewComputeTemplateServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r, err := client.GetComputeTemplate(ctx, &go_client.GetComputeTemplateRequest{
		Name:      name,
		Namespace: opts.namespace,
	})
	if err != nil {
		log.Fatalf("could not list compute template %v", err)
	}

	rows := [][]string{
		convertComputeTemplatToString(r),
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "CPU", "Memory", "GPU", "GPU-Accelerator"})
	table.AppendBulk(rows)
	table.Render()

	return nil
}

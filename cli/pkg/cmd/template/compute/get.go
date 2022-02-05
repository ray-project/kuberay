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
)

func newCmdGet() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <compute template name>",
		Short: "Get a compute template by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getComputeTemplate(args[0])
		},
	}

	return cmd
}

func getComputeTemplate(name string) error {
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
		Name: name,
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

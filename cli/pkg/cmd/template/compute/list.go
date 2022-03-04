package compute

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
	"github.com/spf13/cobra"
)

func newCmdList() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all compute templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listComputeTemplates()
		},
	}

	return cmd
}

func listComputeTemplates() error {
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

	r, err := client.ListComputeTemplates(ctx, &go_client.ListComputeTemplatesRequest{})
	if err != nil {
		log.Fatalf("could not list compute template %v", err)
	}
	computeTemplates := r.GetComputeTemplates()
	rows := convertComputeTemplatesToStrings(computeTemplates)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "CPU", "Memory", "GPU", "GPU-Accelerator"})
	table.AppendBulk(rows)
	table.Render()

	return nil
}

func convertComputeTemplatesToStrings(computeTemplates []*go_client.ComputeTemplate) [][]string {
	var data [][]string

	for _, r := range computeTemplates {
		data = append(data, convertComputeTemplatToString(r))
	}

	return data

}

func convertComputeTemplatToString(r *go_client.ComputeTemplate) []string {
	line := []string{r.GetName(), strconv.Itoa(int(r.GetCpu())), strconv.Itoa(int(r.Memory)),
		strconv.Itoa(int(r.GetGpu())), r.GetGpuAccelerator()}
	return line
}

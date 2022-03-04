package cluster

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
		Use:   "get <cluster id>",
		Short: "Get a ray cluster by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getCluster(args[0])
		},
	}

	return cmd
}

func getCluster(name string) error {
	// Get gRPC connection
	conn, err := cmdutil.GetGrpcConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	// build gRPC client
	client := go_client.NewClusterServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r, err := client.GetCluster(ctx, &go_client.GetClusterRequest{
		Name: name,
	})
	if err != nil {
		log.Fatalf("could not list clusters: %v", err)
	}
	row, nWorkGroups := convertClusterToString(r)
	header := []string{"Name", "User", "Namespace", "Created At", "Version", "Environment", "Head Image", "Head Compute Template", "Head Service Type"}
	for i := 0; i < nWorkGroups; i++ {
		header = append(header, "Worker Group Name", "Worker Image", "Worker ComputeTemplate")
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)
	table.Append(row)
	table.Render()

	return nil
}

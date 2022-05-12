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
	"k8s.io/klog/v2"
)

type GetOptions struct {
	namespace string
}

func newCmdGet() *cobra.Command {
	opts := GetOptions{}

	cmd := &cobra.Command{
		Use:   "get <cluster id>",
		Short: "Get a ray cluster by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getCluster(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "",
		"kubernetes namespace where the cluster is provisioned")
	if err := cmd.MarkFlagRequired("namespace"); err != nil {
		klog.Warning(err)
	}

	return cmd
}

func getCluster(name string, opts GetOptions) error {
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
		Name:      name,
		Namespace: opts.namespace,
	})
	if err != nil {
		log.Fatalf("could not get cluster %v: %v", name, err)
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

package cluster

import (
	"context"
	"log"
	"time"

	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
	"github.com/spf13/cobra"
)

func newCmdDelete() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <cluster name>",
		Short: "Delete a ray cluster by name",
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteCluster(args[0])
		},
	}

	return cmd
}

func deleteCluster(name string) error {
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

	request := &go_client.DeleteClusterRequest{
		Name: name,
	}
	if _, err := client.DeleteCluster(ctx, request); err != nil {
		log.Fatalf("could not delete cluster %v", err)
	}

	log.Printf("cluster %v has been deleted", name)
	return nil
}

package cluster

import (
	"context"
	"log"
	"time"

	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type DeleteOptions struct {
	namespace string
}

func newCmdDelete() *cobra.Command {
	opts := DeleteOptions{}

	cmd := &cobra.Command{
		Use:   "delete <cluster name>",
		Short: "Delete a ray cluster by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteCluster(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "",
		"kubernetes namespace where the cluster is provisioned")
	if err := cmd.MarkFlagRequired("namespace"); err != nil {
		klog.Warning(err)
	}

	return cmd
}

func deleteCluster(name string, opts DeleteOptions) error {
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
		Name:      name,
		Namespace: opts.namespace,
	}
	if _, err := client.DeleteCluster(ctx, request); err != nil {
		log.Fatalf("could not delete cluster %v", err)
	}

	log.Printf("cluster %v has been deleted", name)
	return nil
}

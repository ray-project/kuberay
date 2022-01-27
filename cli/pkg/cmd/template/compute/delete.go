package compute

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
		Use:   "delete <compute template name>",
		Short: "Delete a compute template by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteComputeTemplate(args[0])
		},
	}

	return cmd
}

func deleteComputeTemplate(name string) error {
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

	request := &go_client.DeleteComputeTemplateRequest{
		Name: name,
	}
	if _, err := client.DeleteComputeTemplate(ctx, request); err != nil {
		log.Fatalf("could not delete compute template %v", err)
	}

	log.Printf("compute template %v has been deleted", name)
	return nil
}

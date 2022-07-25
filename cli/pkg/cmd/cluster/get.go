package cluster

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/ray-project/kuberay/cli/pkg/cmdutil"
	"github.com/ray-project/kuberay/proto/go_client"
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

	r, err := client.GetClusterStatus(ctx, &go_client.GetClusterRequest{
		Name:      name,
		Namespace: opts.namespace,
	})
	if err != nil {
		log.Fatalf("could not get cluster %v: %v", name, err)
	}
	row, nWorkGroups := convertClusterToString(r.Cluster)
	header := []string{"Name", "Namespace", "User", "Version", "Environment", "Created At", "Head Image", "Head Compute Template", "Head Service Type"}

	for i := 0; i < nWorkGroups; i++ {
		header = append(header, "Worker Group Name", "Worker Image", "Worker ComputeTemplate")
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)
	table.Append(row)
	table.Render()

	fmt.Println()

	headSpec := r.Cluster.ClusterSpec.HeadGroupSpec
	headHeader := []string{"HEAD IMAGE", "Head Compute Template", "Head Service Type", "POD NAME", "POD IP"}
	headLine := []string{headSpec.GetImage(), headSpec.GetComputeTemplate(), headSpec.GetServiceType()}
	headPodList := r.HeadGroupStatus.PodList
	if len(headPodList) > 0 {
		headLine = append(headLine, headPodList[0].PodName, headPodList[0].PodIp)
	} else {
		headLine = append(headLine, "", "")
	}
	headTable := tablewriter.NewWriter(os.Stdout)
	headTable.SetHeader(headHeader)
	headTable.Append(headLine)
	headTable.Render()

	fmt.Println()

	workerGroupStatusMap := make(map[string]*go_client.WorkerGroupStatus)
	for _, wgs := range r.WorkerGroupStatus {
		workerGroupStatusMap[wgs.GroupName] = wgs
	}

	for _, workGroup := range r.Cluster.ClusterSpec.WorkerGroupSpec {
		workMetaTable := tablewriter.NewWriter(os.Stdout)
		workMetaTable.SetHeader([]string{"WORKER GROUP NAME", "WORKER IMAGE", "WORKER COMPUTETEMPLATE"})
		workMetaTable.Append([]string{workGroup.GetGroupName(), workGroup.GetImage(), workGroup.GetComputeTemplate()})
		workMetaTable.Render()

		workTable := tablewriter.NewWriter(os.Stdout)
		workTable.SetHeader([]string{"POD NAME", "POD IP"})
		wgs, ok := workerGroupStatusMap[workGroup.GroupName]
		if ok {
			for _, pod := range wgs.PodList {
				workTable.Append([]string{pod.PodName, pod.PodIp})
			}
		} else {
			workTable.Append([]string{"", ""})
		}
		workTable.Render()
		fmt.Println()
	}

	return nil
}

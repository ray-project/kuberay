package get

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/client"
	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util/completion"
)

type GetTokenOptions struct {
	cmdFactory cmdutil.Factory
	ioStreams  *genericclioptions.IOStreams
	namespace  string
	secret     string
}

func NewGetTokenOptions(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *GetTokenOptions {
	return &GetTokenOptions{
		cmdFactory: cmdFactory,
		ioStreams:  &streams,
	}
}

func NewGetTokenCommand(cmdFactory cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	options := NewGetTokenOptions(cmdFactory, streams)

	cmd := &cobra.Command{
		Use:               "token [SECRET NAME]",
		Aliases:           []string{"token"},
		Short:             "Get the auth token from the secret.",
		SilenceUsage:      true,
		ValidArgsFunction: completion.RayClusterCompletionFunc(cmdFactory),
		Args:              cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(args, cmd); err != nil {
				return err
			}
			// running cmd.Execute or cmd.ExecuteE sets the context, which will be done by root
			k8sClient, err := client.NewClient(cmdFactory)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			return options.Run(cmd.Context(), k8sClient)
		},
	}
	return cmd
}

func (options *GetTokenOptions) Complete(args []string, cmd *cobra.Command) error {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}
	options.namespace = namespace
	if options.namespace == "" {
		options.namespace = "default"
	}

	if len(args) >= 1 {
		options.secret = args[0]
	} else {
		return fmt.Errorf("secret name is required")
	}

	return nil
}

func (options *GetTokenOptions) Run(ctx context.Context, k8sClient client.Client) error {
	secret, err := k8sClient.KubernetesClient().CoreV1().Secrets(options.namespace).Get(ctx, options.secret, v1.GetOptions{})
	if err != nil {
		return err
	}
	fmt.Fprint(options.ioStreams.Out, string(secret.Data["auth_token"]))
	return nil
}

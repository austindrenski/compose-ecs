package main

import (
	"context"
	"github.com/austindrenski/compose-ecs/ecs"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/cmd/compose"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	plugin.Run(func(cli command.Cli) *cobra.Command {
		return &cobra.Command{
			Short: "Compose ECS",
			Long:  "Convert Docker Compose files into CloudFormation templates.",
			Use:   "ecs",
			RunE: compose.Adapt(func(ctx context.Context, args []string) error {
				cfg, err := config.LoadDefaultConfig(ctx)

				if err != nil {
					return err
				}

				cmd := compose.RootCommand(cli, ecs.NewService(cfg, cli))
				cmd.SetArgs(args)
				cmd.SetContext(ctx)

				return cmd.Execute()
			}),
		}
	}, manager.Metadata{
		SchemaVersion: "0.1.0",
		Vendor:        "austin@austindrenski.io",
		Version:       version,
	})
}

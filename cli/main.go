package main

import (
	"fmt"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose-ecs/ecs"
	"github.com/docker/compose-ecs/internal"
	"github.com/docker/compose/v2/cmd/compatibility"
	commands "github.com/docker/compose/v2/cmd/compose"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	if plugin.RunningStandalone() {
		os.Args = append([]string{"docker"}, compatibility.Convert(os.Args[1:])...)
	}

	plugin.Run(func(dockerCli command.Cli) *cobra.Command {

		service, err := ecs.NewComposeECS()
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}

		root := &cobra.Command{
			Use:              "compose-ecs",
			SilenceErrors:    true,
			SilenceUsage:     true,
			TraverseChildren: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					return cmd.Help()
				}
				return fmt.Errorf("unknown command: %q", args[0])
			},
		}

		root.AddCommand(&cobra.Command{
			Use:   "version",
			Short: "Show the Docker version information",
			Args:  cobra.MaximumNArgs(0),
			RunE: func(cmd *cobra.Command, _ []string) error {
				fmt.Printf("Compose ECS %s\n", internal.Version)
				return nil
			},
		})

		rootCommand := commands.RootCommand(dockerCli, service)

		rootCommand.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
			return cli.StatusError{
				StatusCode: compose.CommandSyntaxFailure.ExitCode,
				Status:     err.Error(),
			}
		})

		return rootCommand
	}, manager.Metadata{
		SchemaVersion: "0.1.0",
		Vendor:        "austin@austindrenski.io",
		Version:       internal.Version,
	})
}

package ecs

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v2/pkg/compose"

	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v2/pkg/api"
	"sigs.k8s.io/kustomize/kyaml/yaml"
	"sigs.k8s.io/kustomize/kyaml/yaml/merge2"
)

type service struct {
	api.Service
	sdk sdk
}

var _ api.Service = &service{}

func NewService(cfg aws.Config, cli command.Cli) api.Service {
	return service{
		Service: compose.NewComposeService(cli),
		sdk: sdk{
			cfg:            cfg,
			cloudformation: cloudformation.NewFromConfig(cfg),
			s3:             s3.NewFromConfig(cfg),
		},
	}
}

func (b service) Config(ctx context.Context, project *types.Project, options api.ConfigOptions) ([]byte, error) {
	if options.Format != "yaml" {
		return nil, fmt.Errorf("format %q is not supported", options.Format)
	}

	template, err := convert(project)
	if err != nil {
		return nil, err
	}

	bytes, err := template.YAML()
	if err != nil {
		return nil, err
	}

	x, ok := project.Extensions[extensionCloudFormation]
	if !ok {
		return bytes, nil
	}

	nodes, err := yaml.Parse(string(bytes))
	if err != nil {
		return nil, err
	}

	bytes, err = yaml.Marshal(x)
	if err != nil {
		return nil, err
	}

	overlay, err := yaml.Parse(string(bytes))
	if err != nil {
		return nil, err
	}

	nodes, err = merge2.Merge(overlay, nodes, yaml.MergeOptions{
		ListIncreaseDirection: yaml.MergeOptionsListPrepend,
	})
	if err != nil {
		return nil, err
	}

	s, err := nodes.String()
	if err != nil {
		return nil, err
	}

	return []byte(s), err
}

func (b service) Down(ctx context.Context, projectName string, options api.DownOptions) error {
	return api.ErrNotImplemented
}

func (b service) Up(ctx context.Context, project *types.Project, _ api.UpOptions) error {
	template, err := b.Config(ctx, project, api.ConfigOptions{
		Format: "yaml",
	})
	if err != nil {
		return err
	}

	if exists, err := b.sdk.StackExists(ctx, project.Name); err != nil {
		return err
	} else if !exists {
		return b.sdk.CreateStack(ctx, project.Name, b.sdk.cfg.Region, template)
	}

	if changeset, err := b.sdk.CreateChangeSet(ctx, project.Name, b.sdk.cfg.Region, template); err != nil {
		return err
	} else {
		return b.sdk.UpdateStack(ctx, changeset)
	}
}

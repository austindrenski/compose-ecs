/*
   Copyright 2020 Docker Compose CLI authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package ecs

import (
	"context"
	"github.com/compose-spec/compose-go/v2/types"
	"os"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/docker/compose/v2/pkg/api"
)

func NewComposeECS() (*ComposeECS, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Profile:           os.Getenv("AWS_PROFILE"),
	})
	if err != nil {
		return nil, err
	}

	sdk := newSDK(sess)
	return &ComposeECS{
		Region: *sess.Config.Region,
		aws:    sdk,
	}, nil
}

type ComposeECS struct {
	Region string
	aws    API
}

func (b *ComposeECS) Build(ctx context.Context, project *types.Project, options api.BuildOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Push(ctx context.Context, project *types.Project, options api.PushOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Pull(ctx context.Context, project *types.Project, options api.PullOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Create(ctx context.Context, project *types.Project, options api.CreateOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Start(ctx context.Context, projectName string, options api.StartOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Restart(ctx context.Context, projectName string, options api.RestartOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Stop(ctx context.Context, projectName string, options api.StopOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Up(ctx context.Context, project *types.Project, options api.UpOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Config(ctx context.Context, project *types.Project, options api.ConfigOptions) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Kill(ctx context.Context, projectName string, options api.KillOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) RunOneOffContainer(ctx context.Context, project *types.Project, opts api.RunOptions) (int, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Remove(ctx context.Context, projectName string, options api.RemoveOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Attach(ctx context.Context, projectName string, options api.AttachOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Copy(ctx context.Context, projectName string, options api.CopyOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Port(ctx context.Context, projectName string, service string, port uint16, options api.PortOptions) (string, int, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Publish(ctx context.Context, project *types.Project, repository string, options api.PublishOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) MaxConcurrency(parallel int) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) DryRunMode(ctx context.Context, dryRun bool) (context.Context, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Watch(ctx context.Context, project *types.Project, services []string, options api.WatchOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Viz(ctx context.Context, project *types.Project, options api.VizOptions) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Wait(ctx context.Context, projectName string, options api.WaitOptions) (int64, error) {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) Scale(ctx context.Context, project *types.Project, options api.ScaleOptions) error {
	//TODO implement me
	panic("implement me")
}

func (b *ComposeECS) ComposeService() api.Service {
	return b
}

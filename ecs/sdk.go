package ecs

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cloudformationtypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/hashicorp/go-uuid"
)

type sdk struct {
	cfg            aws.Config
	cloudformation *cloudformation.Client
	s3             *s3.Client
}

func (s sdk) CreateChangeSet(ctx context.Context, name string, region string, template []byte) (string, error) {
	update := fmt.Sprintf("Update%s", time.Now().Format("2006-01-02-15-04-05"))

	changeset, err := s.withTemplate(ctx, name, template, region, func(body *string, url *string) (string, error) {
		changeset, err := s.cloudformation.CreateChangeSet(ctx, &cloudformation.CreateChangeSetInput{
			ChangeSetName: aws.String(update),
			ChangeSetType: cloudformationtypes.ChangeSetTypeUpdate,
			StackName:     aws.String(name),
			TemplateBody:  body,
			TemplateURL:   url,
			Capabilities:  []cloudformationtypes.Capability{cloudformationtypes.CapabilityCapabilityIam},
		})
		if err != nil {
			return "", err
		}
		return *changeset.Id, err
	})
	if err != nil {
		return "", err
	}

	desc, err := s.cloudformation.DescribeChangeSet(ctx, &cloudformation.DescribeChangeSetInput{
		ChangeSetName: aws.String(update),
		StackName:     aws.String(name),
	})
	if desc.Status == cloudformationtypes.ChangeSetStatusDeleteFailed {
		return changeset, fmt.Errorf(*desc.StatusReason)
	}

	return changeset, err
}

func (s sdk) CreateStack(ctx context.Context, name string, region string, template []byte) error {
	_, err := s.withTemplate(ctx, name, template, region, func(body *string, url *string) (string, error) {
		stack, err := s.cloudformation.CreateStack(ctx, &cloudformation.CreateStackInput{
			OnFailure:        cloudformationtypes.OnFailureDelete,
			StackName:        aws.String(name),
			TemplateBody:     body,
			TemplateURL:      url,
			TimeoutInMinutes: nil,
			Capabilities:     []cloudformationtypes.Capability{cloudformationtypes.CapabilityCapabilityIam},
			Tags: []cloudformationtypes.Tag{
				{
					Key:   aws.String(api.ProjectLabel),
					Value: aws.String(name),
				},
			},
		})
		if err != nil {
			return "", err
		}
		return *stack.StackId, nil
	})
	return err
}

func (s sdk) StackExists(ctx context.Context, name string) (bool, error) {
	stacks, err := s.cloudformation.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(name),
	})
	if err != nil {
		if strings.HasPrefix(err.Error(), fmt.Sprintf("ValidationError: Stack with ID %s does not exist", name)) {
			return false, nil
		}
		return false, nil
	}
	return len(stacks.Stacks) > 0, nil
}

func (s sdk) UpdateStack(ctx context.Context, changeset string) error {
	desc, err := s.cloudformation.DescribeChangeSet(ctx, &cloudformation.DescribeChangeSetInput{
		ChangeSetName: aws.String(changeset),
	})
	if err != nil {
		return err
	}

	if strings.HasPrefix(*desc.StatusReason, "The submitted information didn't contain changes.") {
		return nil
	}

	_, err = s.cloudformation.ExecuteChangeSet(ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName: aws.String(changeset),
	})
	return err
}

func (s sdk) withTemplate(ctx context.Context, name string, template []byte, region string, uploadedTemplateFunc func(body *string, url *string) (string, error)) (string, error) {
	const cloudformationBytesLimit = 51200

	if len(template) < cloudformationBytesLimit {
		return uploadedTemplateFunc(aws.String(string(template)), nil)
	}

	key, err := uuid.GenerateUUID()
	if err != nil {
		return "", err
	}
	bucket := "com.docker.compose." + key

	var configuration *s3types.CreateBucketConfiguration
	if region != "us-east-1" {
		configuration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	_, err = s.s3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                    aws.String(bucket),
		CreateBucketConfiguration: configuration,
	})
	if err != nil {
		return "", err
	}

	upload, err := manager.NewUploader(s.s3).Upload(ctx, &s3.PutObjectInput{
		Key:         aws.String("template.yaml"),
		Body:        bytes.NewReader(template),
		Bucket:      aws.String(bucket),
		ContentType: aws.String("application/x-yaml"),
		Tagging:     aws.String(name),
	})

	if err != nil {
		return "", err
	}

	defer func() {
		_, _ = s.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    aws.String(bucket),
			Key:       aws.String("template.yaml"),
			VersionId: upload.VersionID,
		})
		_, _ = s.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
	}()

	return uploadedTemplateFunc(nil, aws.String(upload.Location))
}

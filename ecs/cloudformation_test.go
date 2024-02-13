package ecs

import (
	"reflect"
	"testing"

	"github.com/awslabs/goformation/v4/cloudformation"
	"github.com/awslabs/goformation/v4/cloudformation/applicationautoscaling"
	"github.com/awslabs/goformation/v4/cloudformation/ec2"
	"github.com/awslabs/goformation/v4/cloudformation/ecs"
	"github.com/awslabs/goformation/v4/cloudformation/elasticloadbalancingv2"
	"github.com/awslabs/goformation/v4/cloudformation/iam"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v2/pkg/api"
	"gotest.tools/v3/assert"
)

func TestAutoScaling(t *testing.T) {
	template := convertYaml(t, `
services:
  foo:
    image: hello_world
    deploy:
      x-aws-autoscaling:
        cpu: 75
        max: 10
`, nil)
	target := template.Resources["FooScalableTarget"].(*applicationautoscaling.ScalableTarget)
	assert.Check(t, target != nil)            //nolint:staticcheck
	assert.Check(t, target.MaxCapacity == 10) //nolint:staticcheck

	policy := template.Resources["FooScalingPolicy"].(*applicationautoscaling.ScalingPolicy)
	assert.Check(t, policy != nil)                                                              //nolint:staticcheck
	assert.Check(t, policy.TargetTrackingScalingPolicyConfiguration.TargetValue == float64(75)) //nolint:staticcheck
}

func TestRollingUpdateLimits(t *testing.T) {
	template := convertYaml(t, `
services:
  foo:
    image: hello_world
    deploy:
      replicas: 4 
      update_config:
        parallelism: 2
`, nil)
	service := template.Resources["FooService"].(*ecs.Service)
	assert.Check(t, service.DeploymentConfiguration.MaximumPercent == 150)
	assert.Check(t, service.DeploymentConfiguration.MinimumHealthyPercent == 50)
}

func TestRolePolicy(t *testing.T) {
	template := convertYaml(t, `
services:
  foo:
    image: hello_world
    x-aws-pull_credentials: "secret"
`, nil)
	x := template.Resources["FooTaskExecutionRole"]
	assert.Check(t, x != nil)
	role := *(x.(*iam.Role))
	assert.Check(t, role.ManagedPolicyArns[0] == "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly")
	assert.Check(t, role.ManagedPolicyArns[1] == "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy")
	// We expect an extra policy has been created for x-aws-pull_credentials
	assert.Check(t, len(role.Policies) == 1)
	policy := role.Policies[0].PolicyDocument
	expected := []string{"secretsmanager:GetSecretValue", "ssm:GetParameters", "kms:Decrypt"}
	assert.DeepEqual(t, expected, policy.(map[string]interface{})["Statement"].([]map[string]interface{})[0]["Action"])
	assert.DeepEqual(t, []string{"secret"}, policy.(map[string]interface{})["Statement"].([]map[string]interface{})[0]["Resource"])
}

func TestMapNetworksToSecurityGroups(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: hello_world
    networks:
      - front-tier
      - back-tier

networks:
  front-tier:
    name: public
  back-tier:
    internal: true
`, nil)
	assert.Check(t, template.Resources["FronttierNetwork"] != nil)
	assert.Check(t, template.Resources["BacktierNetwork"] != nil)
	assert.Check(t, template.Resources["BacktierNetworkIngress"] != nil)
	i := template.Resources["FronttierNetworkIngress"]
	assert.Check(t, i != nil)
	ingress := *i.(*ec2.SecurityGroupIngress)
	assert.Check(t, ingress.SourceSecurityGroupId == cloudformation.Ref("FronttierNetwork"))

}

func TestLoadBalancerTypeApplication(t *testing.T) {
	cases := []string{
		`services:
  test:
    image: nginx
    ports:
      - 80:80
`,
		`services:
  test:
    image: nginx
    ports:
      - target: 8080
        x-aws-protocol: http
`,
	}
	for _, y := range cases {
		template := convertYaml(t, y, nil)
		lb := template.Resources["LoadBalancer"]
		assert.Check(t, lb != nil)
		loadBalancer := *lb.(*elasticloadbalancingv2.LoadBalancer)
		assert.Check(t, len(loadBalancer.Name) <= 32)
		assert.Check(t, loadBalancer.Type == "application")
		assert.Check(t, len(loadBalancer.SecurityGroups) > 0)
	}
}

func TestNoLoadBalancerIfNoPortExposed(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
  foo:
    image: bar
`, nil)
	for _, r := range template.Resources {
		assert.Check(t, r.AWSCloudFormationType() != "AWS::ElasticLoadBalancingV2::TargetGroup")
		assert.Check(t, r.AWSCloudFormationType() != "AWS::ElasticLoadBalancingV2::Listener")
		assert.Check(t, r.AWSCloudFormationType() != "AWS::ElasticLoadBalancingV2::PortPublisher")
	}
}

func TestServiceReplicas(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      replicas: 10
`, nil)
	s := template.Resources["TestService"]
	assert.Check(t, s != nil)
	service := *s.(*ecs.Service)
	assert.Check(t, service.DesiredCount == 10)
}

func TestTaskSizeConvert(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
`, nil)
	def := template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "256")
	assert.Equal(t, def.Memory, "512")

	template = convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 2048M
`, nil)
	def = template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "512")
	assert.Equal(t, def.Memory, "2048")

	template = convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 8192M
`, nil)
	def = template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "4096")
	assert.Equal(t, def.Memory, "8192")

	template = convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 792Mb
`, nil)
	def = template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "4000")
	assert.Equal(t, def.Memory, "792")

	template = convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      resources:
`, nil)
	def = template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "")
	assert.Equal(t, def.Memory, "")

	template = convertYaml(t, `
services:
  test:
    image: nginx
    deploy:
      resources:
        reservations:
          devices: 
            - capabilities: [gpu]
              count: 2
`, nil)
	def = template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	assert.Equal(t, def.Cpu, "")
	assert.Equal(t, def.Memory, "")
}

func TestLoadBalancerTypeNetwork(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
    ports:
      - 80:80
      - 88:88
`, nil)
	lb := template.Resources["LoadBalancer"]
	assert.Check(t, lb != nil)
	loadBalancer := *lb.(*elasticloadbalancingv2.LoadBalancer)
	assert.Check(t, loadBalancer.Type == "network")
}

func TestUseExternalNetwork(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
networks:
  default:
    external: true
    name: sg-123abc
`, nil)
	assert.Check(t, template.Resources["DefaultNetwork"] == nil)
	assert.Check(t, template.Resources["DefaultNetworkIngress"] == nil)
	s := template.Resources["TestService"].(*ecs.Service)
	assert.Check(t, s != nil)                                                                    //nolint:staticcheck
	assert.Check(t, s.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups[0] == "sg-123abc") //nolint:staticcheck
}

func TestServiceMapping(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: "image"
    command: "command"
    entrypoint: "entrypoint"
    environment:
      - "FOO=BAR"
    user: "user"
`, nil)
	def := template.Resources["TestTaskDefinition"].(*ecs.TaskDefinition)
	container := getMainContainer(def, t)
	assert.Equal(t, container.Image, "image")
	assert.Equal(t, container.Command[0], "command")
	assert.Equal(t, container.EntryPoint[0], "entrypoint")
	assert.Equal(t, get(container.Environment, "FOO"), "BAR")
	assert.Equal(t, container.User, "user")
}

func get(l []ecs.TaskDefinition_KeyValuePair, name string) string {
	for _, e := range l {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func TestResourcesHaveProjectTagSet(t *testing.T) {
	template := convertYaml(t, `
services:
  test:
    image: nginx
    ports:
      - 80:80
      - 88:88
`, nil)
	for _, r := range template.Resources {
		tags := reflect.Indirect(reflect.ValueOf(r)).FieldByName("Tags")
		if !tags.IsValid() {
			continue
		}
		for i := 0; i < tags.Len(); i++ {
			k := tags.Index(i).FieldByName("Key").String()
			v := tags.Index(i).FieldByName("Value").String()
			if k == api.ProjectLabel {
				assert.Equal(t, v, t.Name())
			}
		}
	}
}

func TestTemplateMetadata(t *testing.T) {
	template := convertYaml(t, `
x-aws-cluster: "arn:aws:ecs:region:account:cluster/name"
services:
  test:
    image: nginx
`, nil)
	assert.Equal(t, template.Metadata["Cluster"], "arn:aws:ecs:region:account:cluster/name")
}

func convertYaml(t *testing.T, yaml string, assertErr error, fn ...func(m *sdk)) *cloudformation.Template {
	project := loadConfig(t, yaml)

	m := sdk{}
	for _, f := range fn {
		f(&m)
	}

	template, err := convert(project)
	if assertErr == nil {
		assert.NilError(t, err)
	} else {
		assert.Error(t, err, assertErr.Error())
	}

	return template
}

func loadConfig(t *testing.T, yaml string) *types.Project {
	dict, err := loader.ParseYAML([]byte(yaml))
	assert.NilError(t, err)
	model, err := loader.Load(types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{
			{Config: dict},
		},
	}, func(options *loader.Options) {
		options.SetProjectName(t.Name(), true)
	})
	assert.NilError(t, err)
	return model
}

func getMainContainer(def *ecs.TaskDefinition, t *testing.T) ecs.TaskDefinition_ContainerDefinition {
	for _, c := range def.ContainerDefinitions {
		if c.Essential {
			return c
		}
	}
	t.Fail()
	return def.ContainerDefinitions[0]
}

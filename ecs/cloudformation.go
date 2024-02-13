package ecs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/awslabs/goformation/v4/cloudformation"
	"github.com/awslabs/goformation/v4/cloudformation/applicationautoscaling"
	"github.com/awslabs/goformation/v4/cloudformation/ec2"
	"github.com/awslabs/goformation/v4/cloudformation/ecs"
	"github.com/awslabs/goformation/v4/cloudformation/elasticloadbalancingv2"
	"github.com/awslabs/goformation/v4/cloudformation/iam"
	"github.com/awslabs/goformation/v4/cloudformation/logs"
	"github.com/awslabs/goformation/v4/cloudformation/servicediscovery"
	"github.com/awslabs/goformation/v4/cloudformation/tags"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/opts"
	"github.com/docker/compose/v2/pkg/api"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	extensionAutoScaling     = "x-aws-autoscaling"
	extensionCloudFormation  = "x-aws-cloudformation"
	extensionLoadBalancer    = "x-aws-loadbalancer"
	extensionPullCredentials = "x-aws-pull_credentials"
	extensionRole            = "x-aws-role"
	extensionSubnets         = "x-aws-subnets"
	extensionVpc             = "x-aws-vpc"
)

type resources struct {
	loadBalancer   string
	securityGroups map[string]string
	subnets        []string
	vpc            string // shouldn't this also be an awsResource ?
}

func checkCompatibility(project *types.Project) error {
	for _, config := range project.Configs {
		return fmt.Errorf("configs are not supported: %s", config.Name)
	}

	for _, network := range project.Networks {
		if strings.HasSuffix(network.Name, "_default") {
			continue
		}

		if !network.External {
			return fmt.Errorf("only external networks are supported: %s", network.Name)
		}
	}

	for _, secret := range project.Secrets {
		if !secret.External {
			return fmt.Errorf("only external secrets are supported: %s", secret.Name)
		}
	}

	// TODO: bring back some level of compatibility validation
	//for _, service := range project.Services {
	//}

	for _, volume := range project.Volumes {
		return fmt.Errorf("volumes are not supported: %s", volume.Name)
	}

	return nil
}

func convert(project *types.Project) (*cloudformation.Template, error) {
	if err := checkCompatibility(project); err != nil {
		return nil, err
	}

	resources := resources{}
	template := cloudformation.NewTemplate()

	if x, ok := project.Extensions[extensionLoadBalancer]; ok {
		resources.loadBalancer = x.(string)
	} else {
		return nil, fmt.Errorf("missing required %s", extensionLoadBalancer)
	}

	if x, ok := project.Extensions[extensionSubnets]; ok {
		for _, subnet := range x.([]interface{}) {
			resources.subnets = append(resources.subnets, subnet.(string))
		}
	} else {
		return nil, fmt.Errorf("missing required %s", extensionSubnets)
	}

	if x, ok := project.Extensions[extensionVpc]; ok {
		resources.vpc = x.(string)
	} else {
		return nil, fmt.Errorf("missing required %s", extensionVpc)
	}

	template.Resources["CloudMap"] = &servicediscovery.PrivateDnsNamespace{
		Description: fmt.Sprintf("Service Map for Docker Compose project %s", project.Name),
		Name:        fmt.Sprintf("%s.local", project.Name),
		Tags:        projectTags(project),
		Vpc:         resources.vpc,
	}
	template.Resources["Cluster"] = &ecs.Cluster{
		ClusterName: project.Name,
		Tags:        projectTags(project),
	}
	template.Resources["LogGroup"] = &logs.LogGroup{
		LogGroupName:    fmt.Sprintf("/docker-compose/%s", project.Name),
		RetentionInDays: 0,
	}

	for _, service := range project.Services {
		if err := createService(project, service, template, resources); err != nil {
			return nil, err
		}
	}

	return template, nil
}

func createAutoscalingPolicy(project *types.Project, template *cloudformation.Template, service types.ServiceConfig) error {
	if service.Deploy == nil {
		return nil
	}
	v, ok := service.Deploy.Extensions[extensionAutoScaling]
	if !ok {
		return nil
	}

	marshalled, err := json.Marshal(v)
	if err != nil {
		return err
	}

	config := struct {
		Memory int `json:"memory,omitempty"`
		CPU    int `json:"cpu,omitempty"`
		Min    int `json:"min,omitempty"`
		Max    int `json:"max,omitempty"`
	}{}

	err = json.Unmarshal(marshalled, &config)
	if err != nil {
		return err
	}

	if config.Memory != 0 && config.CPU != 0 {
		return fmt.Errorf("%s can't be set with both cpu and memory targets", extensionAutoScaling)
	}
	if config.Max == 0 {
		return fmt.Errorf("%s MUST define max replicas", extensionAutoScaling)
	}

	template.Resources[serviceAutoScalingRoleName(service)] = &iam.Role{
		AssumeRolePolicyDocument: map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Action": []string{"sts:AssumeRole"},
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"Service": "application-autoscaling.amazonaws.com",
					},
				},
			},
		},
		Path: "/",
		Policies: []iam.Role_Policy{
			{
				PolicyDocument: map[string]interface{}{
					"Statement": []map[string]interface{}{
						{
							"Action": []string{
								"application-autoscaling:*",
								"cloudwatch:GetMetricStatistics",
								"ecs:DescribeServices",
								"ecs:UpdateService",
							},
							"Effect":   "Allow",
							"Resource": []string{cloudformation.Ref(serviceName(service))},
						},
					},
				},
				PolicyName: "service-autoscaling",
			},
		},
		Tags: serviceTags(project, service),
	}
	template.Resources[serviceScalableTargetName(service)] = &applicationautoscaling.ScalableTarget{
		MaxCapacity:                config.Max,
		MinCapacity:                config.Min,
		ResourceId:                 fmt.Sprintf("service/%s/%s", cloudformation.Ref("Cluster"), cloudformation.GetAtt(serviceName(service), "Name")),
		RoleARN:                    cloudformation.GetAtt(serviceAutoScalingRoleName(service), "Arn"),
		ScalableDimension:          "ecs:service:DesiredCount",
		ServiceNamespace:           "ecs",
		AWSCloudFormationDependsOn: []string{serviceName(service)},
	}

	metric := "ECSServiceAverageCPUUtilization"
	targetPercent := config.CPU

	if config.Memory != 0 {
		metric = "ECSServiceAverageMemoryUtilization"
		targetPercent = config.Memory
	}

	template.Resources[serviceScalingPolicyName(service)] = &applicationautoscaling.ScalingPolicy{
		PolicyType:                     "TargetTrackingScaling",
		PolicyName:                     serviceScalingPolicyName(service),
		ScalingTargetId:                cloudformation.Ref(serviceScalableTargetName(service)),
		StepScalingPolicyConfiguration: nil,
		TargetTrackingScalingPolicyConfiguration: &applicationautoscaling.ScalingPolicy_TargetTrackingScalingPolicyConfiguration{
			PredefinedMetricSpecification: &applicationautoscaling.ScalingPolicy_PredefinedMetricSpecification{
				PredefinedMetricType: metric,
			},
			ScaleOutCooldown: 60,
			ScaleInCooldown:  60,
			TargetValue:      float64(targetPercent),
		},
	}
	return nil
}

func createService(project *types.Project, service types.ServiceConfig, template *cloudformation.Template, resources resources) error {
	var secretArns []string
	if value, ok := service.Extensions[extensionPullCredentials]; ok {
		secretArns = append(secretArns, value.(string))
	}
	for _, secret := range service.Secrets {
		secretArns = append(secretArns, project.Secrets[secret.Source].Name)
	}

	cpu, mem, err := toLimits(service)
	if err != nil {
		panic(err)
	}

	template.Resources[serviceDiscoveryEntryName(service)] = &servicediscovery.Service{
		Description:       fmt.Sprintf("%q service discovery entry in Cloud Map", service.Name),
		HealthCheckConfig: nil,
		HealthCheckCustomConfig: &servicediscovery.Service_HealthCheckCustomConfig{
			FailureThreshold: 1,
		},
		Name:        service.Name,
		NamespaceId: cloudformation.Ref("CloudMap"),
		DnsConfig: &servicediscovery.Service_DnsConfig{
			DnsRecords: []servicediscovery.Service_DnsRecord{
				{
					TTL:  60,
					Type: "A",
				},
			},
			RoutingPolicy: "MULTIVALUE",
		},
	}
	template.Resources[serviceTaskDefinitionName(service)] = &ecs.TaskDefinition{
		ContainerDefinitions: []ecs.TaskDefinition_ContainerDefinition{
			{
				Command:               service.Command,
				DisableNetworking:     false,
				DependsOnProp:         nil,
				DnsSearchDomains:      service.DNSSearch,
				DnsServers:            service.DNS,
				DockerLabels:          service.Labels,
				DockerSecurityOptions: service.SecurityOpt,
				EntryPoint:            service.Entrypoint,
				Environment:           createEnvironment(service),
				Essential:             true,
				ExtraHosts:            nil,
				FirelensConfiguration: nil,
				HealthCheck:           toHealthCheck(service.HealthCheck),
				Hostname:              service.Hostname,
				Image:                 service.Image,
				Interactive:           false,
				Links:                 nil,
				LinuxParameters:       nil,
				LogConfiguration: &ecs.TaskDefinition_LogConfiguration{
					LogDriver: "awslogs",
					Options: map[string]string{
						"awslogs-region":        cloudformation.Ref("AWS::Region"),
						"awslogs-group":         cloudformation.Ref("LogGroup"),
						"awslogs-stream-prefix": project.Name,
					},
				},
				MemoryReservation:      0,
				MountPoints:            nil,
				Name:                   service.Name,
				PortMappings:           toPortMappings(service.Ports),
				Privileged:             service.Privileged,
				PseudoTerminal:         service.Tty,
				ReadonlyRootFilesystem: service.ReadOnly,
				RepositoryCredentials:  createRepositoryCredentials(service),
				ResourceRequirements:   nil,
				Secrets:                createTaskSecrets(project, service),
				StartTimeout:           0,
				StopTimeout:            durationToInt(service.StopGracePeriod),
				SystemControls:         nil,
				Ulimits:                toUlimits(service.Ulimits),
				User:                   service.User,
				VolumesFrom:            nil,
				WorkingDirectory:       service.WorkingDir,
			},
		},
		Cpu:                     cpu,
		ExecutionRoleArn:        cloudformation.Ref(serviceTaskExecutionRoleName(service)),
		Family:                  fmt.Sprintf("%s-%s", project.Name, service.Name),
		IpcMode:                 service.Ipc,
		Memory:                  mem,
		NetworkMode:             "awsvpc",
		PidMode:                 service.Pid,
		PlacementConstraints:    nil,
		ProxyConfiguration:      nil,
		RequiresCompatibilities: []string{"FARGATE"},
		TaskRoleArn:             cloudformation.Ref(fmt.Sprintf("%sTaskRole", normalizeResourceName(service.Name))),
		Volumes:                 nil,
	}
	template.Resources[serviceTaskExecutionRoleName(service)] = &iam.Role{
		AssumeRolePolicyDocument: map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Action": []string{"sts:AssumeRole"},
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"Service": "ecs-tasks.amazonaws.com",
					},
				},
			},
		},
		ManagedPolicyArns: []string{"arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"},
		Policies: []iam.Role_Policy{
			{
				PolicyDocument: map[string]interface{}{
					"Statement": []map[string]interface{}{
						{
							"Action":   []string{"secretsmanager:GetSecretValue"},
							"Effect":   "Allow",
							"Resource": getSecretArns(project, service),
						},
					},
				},
				PolicyName: fmt.Sprintf("%sGrantAccessToSecrets", service.Name),
			},
		},
		Tags: serviceTags(project, service),
	}
	template.Resources[serviceTaskRoleName(service)] = &iam.Role{
		AssumeRolePolicyDocument: map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Action": []string{"sts:AssumeRole"},
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"Service": "ecs-tasks.amazonaws.com",
					},
				},
			},
		},
		Policies: []iam.Role_Policy{
			{
				PolicyDocument: service.Extensions[extensionRole],
				PolicyName:     fmt.Sprintf("%sPolicy", normalizeResourceName(service.Name)),
			},
		},
		Tags: serviceTags(project, service),
	}

	for _, port := range service.Ports {
		for network := range service.Networks {
			template.Resources[fmt.Sprintf("%s%dIngress", normalizeResourceName(network), port.Target)] = &ec2.SecurityGroupIngress{
				CidrIp:      "0.0.0.0/0",
				Description: fmt.Sprintf("%s:%d/%s on %s network", service.Name, port.Target, port.Protocol, network),
				FromPort:    int(port.Target),
				GroupId:     resources.securityGroups[network],
				IpProtocol:  "-1",
				ToPort:      int(port.Target),
			}
		}

		template.Resources[serviceListenerName(service, port)] = &elasticloadbalancingv2.Listener{
			DefaultActions: []elasticloadbalancingv2.Listener_Action{
				{
					ForwardConfig: &elasticloadbalancingv2.Listener_ForwardConfig{
						TargetGroups: []elasticloadbalancingv2.Listener_TargetGroupTuple{
							{
								TargetGroupArn: cloudformation.Ref(serviceTargetGroupName(service, port)),
							},
						},
					},
					Type: "forward",
				},
			},
			LoadBalancerArn: resources.loadBalancer,
			Protocol:        strings.ToUpper(port.Protocol),
			Port:            int(port.Target),
		}
		template.Resources[serviceTargetGroupName(service, port)] = &elasticloadbalancingv2.TargetGroup{
			Port:       int(port.Target),
			Protocol:   strings.ToUpper(port.Protocol),
			Tags:       serviceTags(project, service),
			TargetType: "ip",
			VpcId:      resources.vpc,
		}
	}

	template.Resources[serviceName(service)] = &ecs.Service{
		AWSCloudFormationDependsOn: serviceDependsOn(service),
		Cluster:                    cloudformation.GetAtt("Cluster", "Arn"),
		DesiredCount:               replicas(service),
		DeploymentController: &ecs.Service_DeploymentController{
			Type: "ECS",
		},
		DeploymentConfiguration: &ecs.Service_DeploymentConfiguration{
			MaximumPercent:        200,
			MinimumHealthyPercent: 100,
		},
		LaunchType:    "FARGATE",
		LoadBalancers: createServiceLoadBalancers(service),
		NetworkConfiguration: &ecs.Service_NetworkConfiguration{
			AwsvpcConfiguration: &ecs.Service_AwsVpcConfiguration{
				AssignPublicIp: "ENABLED",
				SecurityGroups: getServiceSecurityGroups(service, resources),
				Subnets:        resources.subnets,
			},
		},
		PlatformVersion:    "LATEST",
		PropagateTags:      "SERVICE",
		SchedulingStrategy: "REPLICA",
		ServiceRegistries: []ecs.Service_ServiceRegistry{
			{
				RegistryArn: cloudformation.GetAtt(serviceDiscoveryEntryName(service), "Arn"),
			},
		},
		Tags:           serviceTags(project, service),
		TaskDefinition: cloudformation.Ref(serviceTaskDefinitionName(service)),
	}

	if err := createAutoscalingPolicy(project, template, service); err != nil {
		return err
	}

	return nil
}

func createServiceLoadBalancers(service types.ServiceConfig) []ecs.Service_LoadBalancer {
	serviceLoadBalancers := make([]ecs.Service_LoadBalancer, len(service.Ports))

	for i, port := range service.Ports {
		serviceLoadBalancers[i] = ecs.Service_LoadBalancer{
			ContainerName:  service.Name,
			ContainerPort:  int(port.Target),
			TargetGroupArn: cloudformation.Ref(serviceTargetGroupName(service, port)),
		}
	}

	return serviceLoadBalancers
}

func createRepositoryCredentials(service types.ServiceConfig) *ecs.TaskDefinition_RepositoryCredentials {
	if value, ok := service.Extensions[extensionPullCredentials]; ok {
		return &ecs.TaskDefinition_RepositoryCredentials{
			CredentialsParameter: value.(string),
		}
	} else {
		return nil
	}
}

func createTaskSecrets(project *types.Project, service types.ServiceConfig) []ecs.TaskDefinition_Secret {
	taskSecrets := make([]ecs.TaskDefinition_Secret, len(service.Secrets))

	for i, s := range service.Secrets {
		if s.Target == "" {
			s.Target = s.Source
		}
		taskSecrets[i] = ecs.TaskDefinition_Secret{
			Name:      s.Target,
			ValueFrom: project.Secrets[s.Source].Name,
		}
	}

	return taskSecrets
}

func replicas(service types.ServiceConfig) int {
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		return *service.Deploy.Replicas
	}

	return 1
}

func serviceName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sService", normalizeResourceName(service.Name))
}

func serviceAutoScalingRoleName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sAutoScalingRole", normalizeResourceName(service.Name))
}

func serviceDependsOn(service types.ServiceConfig) []string {
	var dependsOn []string

	for dependency := range service.DependsOn {
		dependsOn = append(dependsOn, fmt.Sprintf("%sService", normalizeResourceName(dependency)))
	}

	for _, port := range service.Ports {
		dependsOn = append(dependsOn, serviceListenerName(service, port))
	}

	return dependsOn
}

func serviceDiscoveryEntryName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sServiceDiscoveryEntry", normalizeResourceName(service.Name))
}

func serviceListenerName(service types.ServiceConfig, port types.ServicePortConfig) string {
	return fmt.Sprintf("%s%s%dListener", normalizeResourceName(service.Name), strings.ToUpper(port.Protocol), port.Target)
}

func serviceScalableTargetName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sScalableTarget", normalizeResourceName(service.Name))
}

func serviceScalingPolicyName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sScalingPolicy", normalizeResourceName(service.Name))
}

func serviceTaskDefinitionName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sTaskDefinition", normalizeResourceName(service.Name))
}

func serviceTaskExecutionRoleName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sTaskExecutionRole", normalizeResourceName(service.Name))
}

func serviceTargetGroupName(service types.ServiceConfig, port types.ServicePortConfig) string {
	return fmt.Sprintf("%s%s%sTargetGroup", normalizeResourceName(service.Name), strings.ToUpper(port.Protocol), port.Published)
}

func serviceTaskRoleName(service types.ServiceConfig) string {
	return fmt.Sprintf("%sTaskRole", normalizeResourceName(service.Name))
}

var (
	regex = regexp.MustCompile("[^a-zA-Z0-9]+")
	title = cases.Title(language.English)
)

func normalizeResourceName(s string) string {
	return title.String(regex.ReplaceAllString(s, ""))
}

func createEnvironment(service types.ServiceConfig) []ecs.TaskDefinition_KeyValuePair {
	var pairs []ecs.TaskDefinition_KeyValuePair

	for k, v := range service.Environment {
		pairs = append(pairs, ecs.TaskDefinition_KeyValuePair{
			Name:  k,
			Value: *v,
		})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Name < pairs[j].Name
	})

	return pairs
}

const miB = 1024 * 1024

func toLimits(service types.ServiceConfig) (string, string, error) {
	mem, cpu, err := getConfiguredLimits(service)
	if err != nil {
		return "", "", err
	}

	// All possible cpu/mem values for Fargate
	fargateCPUToMem := map[int64][]types.UnitBytes{
		256:  {512, 1024, 2048},
		512:  {1024, 2048, 3072, 4096},
		1024: {2048, 3072, 4096, 5120, 6144, 7168, 8192},
		2048: {4096, 5120, 6144, 7168, 8192, 9216, 10240, 11264, 12288, 13312, 14336, 15360, 16384},
		4096: {8192, 9216, 10240, 11264, 12288, 13312, 14336, 15360, 16384, 17408, 18432, 19456, 20480, 21504, 22528, 23552, 24576, 25600, 26624, 27648, 28672, 29696, 30720},
	}
	cpuLimit := "256"
	memLimit := "512"
	if mem == 0 && cpu == 0 {
		return cpuLimit, memLimit, nil
	}

	var cpus []int64
	for k := range fargateCPUToMem {
		cpus = append(cpus, k)
	}
	sort.Slice(cpus, func(i, j int) bool { return cpus[i] < cpus[j] })

	for _, fargateCPU := range cpus {
		options := fargateCPUToMem[fargateCPU]
		if cpu <= fargateCPU {
			for _, m := range options {
				if mem <= m*miB {
					cpuLimit = strconv.FormatInt(fargateCPU, 10)
					memLimit = strconv.FormatInt(int64(m), 10)
					return cpuLimit, memLimit, nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("the resources requested are not supported by ECS/Fargate")
}

func getConfiguredLimits(service types.ServiceConfig) (types.UnitBytes, int64, error) {
	if service.Deploy == nil {
		return 0, 0, nil
	}

	limits := service.Deploy.Resources.Limits
	if limits == nil {
		limits = service.Deploy.Resources.Reservations
	}
	if limits == nil {
		return 0, 0, nil
	}

	if limits.NanoCPUs == "" {
		return limits.MemoryBytes, 0, nil
	}
	v, err := opts.ParseCPUs(limits.NanoCPUs)
	if err != nil {
		return 0, 0, err
	}

	return limits.MemoryBytes, v / 1e6, nil
}

func getServiceSecurityGroups(service types.ServiceConfig, resources resources) []string {
	var groups []string

	for network := range service.Networks {
		groups = append(groups, resources.securityGroups[network])
	}

	return groups
}

func getSecretArns(project *types.Project, service types.ServiceConfig) []string {
	var secretArns []string

	if value, ok := service.Extensions[extensionPullCredentials]; ok {
		secretArns = append(secretArns, value.(string))
	}

	for _, secret := range service.Secrets {
		secretArns = append(secretArns, project.Secrets[secret.Source].Name)
	}

	return secretArns
}

func toPortMappings(ports []types.ServicePortConfig) []ecs.TaskDefinition_PortMapping {
	if len(ports) == 0 {
		return nil
	}

	m := make([]ecs.TaskDefinition_PortMapping, len(ports))
	for i, p := range ports {
		m[i] = ecs.TaskDefinition_PortMapping{
			ContainerPort: int(p.Target),
			Protocol:      p.Protocol,
		}
	}
	return m
}

func toUlimits(ulimits map[string]*types.UlimitsConfig) []ecs.TaskDefinition_Ulimit {
	if len(ulimits) == 0 {
		return nil
	}

	u := make([]ecs.TaskDefinition_Ulimit, len(ulimits))
	for k, v := range ulimits {
		u = append(u, ecs.TaskDefinition_Ulimit{
			Name:      k,
			SoftLimit: v.Soft,
			HardLimit: v.Hard,
		})
	}
	return u
}

func toHealthCheck(check *types.HealthCheckConfig) *ecs.TaskDefinition_HealthCheck {
	if check == nil {
		return nil
	}
	retries := 0
	if check.Retries != nil {
		retries = int(*check.Retries)
	}
	return &ecs.TaskDefinition_HealthCheck{
		Command:     check.Test,
		Interval:    durationToInt(check.Interval),
		Retries:     retries,
		StartPeriod: durationToInt(check.StartPeriod),
		Timeout:     durationToInt(check.Timeout),
	}
}

func durationToInt(interval *types.Duration) int {
	if interval == nil {
		return 0
	}
	v := int(time.Duration(*interval).Seconds())
	return v
}

func projectTags(project *types.Project) []tags.Tag {
	return []tags.Tag{
		{
			Key:   api.ProjectLabel,
			Value: project.Name,
		},
	}
}

func serviceTags(project *types.Project, service types.ServiceConfig) []tags.Tag {
	return append(projectTags(project),
		tags.Tag{
			Key:   api.ServiceLabel,
			Value: service.Name,
		})
}

//goland:noinspection GoUnusedGlobalVariable
var compatibleComposeAttributes = []string{
	"services.command",
	"services.container_name",
	"services.depends_on",
	"services.deploy",
	"services.deploy.replicas",
	"services.deploy.resources.limits",
	"services.deploy.resources.limits.cpus",
	"services.deploy.resources.limits.memory",
	"services.deploy.update_config",
	"services.deploy.update_config.parallelism",
	"services.entrypoint",
	"services.environment",
	"services.healthcheck",
	"services.healthcheck.interval",
	"services.healthcheck.retries",
	"services.healthcheck.start_period",
	"services.healthcheck.test",
	"services.healthcheck.timeout",
	"services.image",
	"services.labels",
	"services.ports",
	"services.ports.mode",
	"services.ports.target",
	"services.ports.protocol",
	"services.scale",
	"services.secrets",
	"services.secrets.source",
	"services.secrets.target",
	"services.user",
	"secrets.external",
	"secrets.name",
	"networks.external",
	"networks.name",
}

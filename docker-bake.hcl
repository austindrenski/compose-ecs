variable GITHUB_SHA { default = "$GITHUB_SHA" }
variable GITHUB_REF_NAME { default = "$GITHUB_REF_NAME" }
variable GITHUB_REF_TYPE { default = "$GITHUB_REF_TYPE" }
variable REGISTRY { default = "ghcr.io/austindrenski" }
variable VERSION { default = "0.0.1" }

group default {
  targets = [
    "compose_ecs"
  ]
}

target base {
  args = {
    VERSION = version()
  }
  contexts = {
    root = "."
  }
  dockerfile = "Dockerfile"
  output     = [
    "type=image"
  ]
  platforms = [
    "linux/amd64",
    "linux/arm64"
  ]
  pull = true
}

target compose_ecs {
  inherits = [
    target.base.name
  ]
  labels = {
    "org.opencontainers.image.description" = "Docker CLI plugin for converting Compose => CloudFormation"
    "org.opencontainers.image.name"        = "compose-ecs"
    "org.opencontainers.image.title"       = "Docker Compose ECS conversion"
  }
  tags = tags(target.compose_ecs.labels)
}

function tags {
  params = [labels]
  result = [
    equal(version(), VERSION) ? format("%s/%s:%s", REGISTRY, labels["org.opencontainers.image.name"], "latest") : "",
    format("%s/%s:%s", REGISTRY, labels["org.opencontainers.image.name"], version())
  ]
}

function version {
  params = []
  result = equal(GITHUB_REF_TYPE, "tag") ? trimprefix(lower(GITHUB_REF_NAME), "v") : format("%s-ci.%s", VERSION, lower(GITHUB_REF_NAME))
}

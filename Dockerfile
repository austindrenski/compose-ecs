# syntax = docker/dockerfile:1.6
FROM --platform=${BUILDPLATFORM} golang:1.22.0-alpine AS build

WORKDIR /go/src/

COPY --link --from=root go.mod .
COPY --link --from=root go.sum .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY --link --from=root . .

ARG VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w -X 'github.com/austindrenski/compose-ecs/main.version=${VERSION}'" -o /app/compose-ecs ./cmd/main.go

FROM --platform="$TARGETPLATFORM" alpine AS certs

RUN apk add --no-cache --update ca-certificates-bundle

FROM --platform="$TARGETPLATFORM" scratch

COPY --link --from=build /app/compose-ecs /usr/local/bin/
COPY --link --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER 1001:1001

CMD []
ENTRYPOINT ["compose-ecs"]

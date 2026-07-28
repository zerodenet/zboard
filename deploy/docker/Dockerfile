# syntax=docker/dockerfile:1

FROM node:22.23.0-bookworm-slim AS frontend-builder
WORKDIR /src
ARG PNPM_REGISTRY=https://registry.npmmirror.com
ENV PNPM_CONFIG_REGISTRY=${PNPM_REGISTRY}
ENV CI=true
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./frontend/
WORKDIR /src/frontend
RUN pnpm install --frozen-lockfile
COPY frontend/ .
RUN pnpm build

FROM golang:1.26.5-alpine AS backend-builder
WORKDIR /src/backend
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ .
ARG APP_VERSION=v0.0.1
ARG APP_COMMIT=dev
ARG APP_BUILD_TIME=1970-01-01T00:00:00Z
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X github.com/zerodenet/zboard/backend/internal/version.Version=${APP_VERSION} -X github.com/zerodenet/zboard/backend/internal/version.Commit=${APP_COMMIT} -X github.com/zerodenet/zboard/backend/internal/version.BuildTime=${APP_BUILD_TIME}" \
    -o /zboard ./cmd/zboard

FROM alpine:3.20
LABEL org.opencontainers.image.source="https://github.com/zerodenet/zboard"
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
RUN mkdir -p /app/etc
COPY --from=backend-builder /zboard /usr/local/bin/zboard
COPY backend/etc/zboard.yaml.example /app/etc/zboard.yaml
COPY --from=frontend-builder /src/frontend/dist ./web
ENV ZBOARD_WEB_DIR=/app/web
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/zboard", "-f", "/app/etc/zboard.yaml"]

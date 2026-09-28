FROM oven/bun:1.3.14 AS management
WORKDIR /panel
COPY management/package.json management/bun.lock ./
RUN bun install --frozen-lockfile
COPY management/ ./
ENV VERSION=company
RUN bun run build

FROM golang:1.26-bookworm AS gateway
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends build-essential git && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -buildvcs=false -o /CLIProxyAPI ./cmd/server

FROM debian:bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /CLIProxyAPI
COPY --from=gateway /CLIProxyAPI ./CLIProxyAPI
COPY --from=management /panel/dist/index.html ./company-panel/management.html
ENV MANAGEMENT_STATIC_PATH=/CLIProxyAPI/company-panel
ENV TZ=Asia/Shanghai
EXPOSE 8317
CMD ["./CLIProxyAPI", "-config", "/CLIProxyAPI/settings/config.yaml", "-local-model"]

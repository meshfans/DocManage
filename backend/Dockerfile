# =============================================================================
# Stage 1: Build
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# 安装编译依赖（CGO_ENABLED=1 需要 gcc，关闭则不需要）
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

# 复制 go mod/sum（利用 Docker 缓存层加速）
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 交叉编译支持（允许 docker buildx --platform linux/amd64,linux/arm64）
ARG TARGETOS TARGETARCH
ARG CGO_ENABLED=0

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=${CGO_ENABLED} \
    go build -ldflags="\
      -s -w \
      -X doc/utils.Version=production \
    " \
    -o /app/bin/doc-server \
    main.go

# =============================================================================
# Stage 2: Runtime（distroless，无 shell，攻击面最小）
# =============================================================================
FROM gcr.io/distroless/static:nonroot AS runtime

# tzdata 用于日志时区显示（非必需）
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 仅复制二进制和配置目录结构
COPY --from=builder /app/bin/doc-server /app/bin/doc-server

# 运行用户为 nonroot（uid=65532），bin 目录需对运行用户可写（uploads/ 等）
WORKDIR /app/bin

# 运行用户无 shell、无 home、无 login 能力
USER nonroot

ENTRYPOINT ["./doc-server"]

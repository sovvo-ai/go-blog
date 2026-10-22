# ============================================================
# 生产镜像：多阶段构建
#
# 基础镜像固定到「补丁版本 + 摘要」，禁止 latest（TECH_DESIGN 15.1 / 2.3）
# 摘要在本机 docker pull 后由 RepoDigests 取得，换版本时必须重新核对
# 构建：docker build -f deploy/Dockerfile -t blog-app .
# ============================================================

# ---------- 构建阶段 ----------
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder

WORKDIR /src

# 允许构建期覆盖模块代理（内网或受限网络的 CI 需要）
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# 先只拷依赖清单，命中 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

# CGO_ENABLED=0：产出静态二进制，运行时镜像不带 libc
# -trimpath：去掉本机绝对路径，避免泄露构建环境
# -ldflags="-s -w"：去掉符号表与调试信息
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/server ./cmd/server

# ---------- 运行阶段 ----------
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# 不执行 apk add：
#   - CA 证书直接从构建阶段复制（golang 镜像自带）
#   - 时区数据由 time/tzdata 内嵌进二进制，不依赖镜像文件
# 这样运行阶段完全离线可用，也少一层包管理带来的攻击面。
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# addgroup / adduser 是 busybox 内置命令，无需联网
RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/server /usr/local/bin/server
# 迁移脚本随镜像分发，部署时由独立步骤执行（不在应用启动时跑）
COPY --from=builder /src/migrations /app/migrations

USER app

EXPOSE 8080

# 容器健康检查由编排层 healthcheck 负责，镜像内不重复定义，
# 避免与 compose / K8s 的探针策略打架。

ENTRYPOINT ["/usr/local/bin/server"]

# 构建阶段
FROM golang:1.26-alpine AS builder

# 国内网络下用 goproxy.cn；海外构建可传 --build-arg GOPROXY=https://proxy.golang.org,direct
ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0 GOOS=linux

WORKDIR /src

# 先只拷依赖清单：源码变动不会让依赖下载层失效，重建快很多
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -trimpath 去掉构建机的绝对路径，避免把本地目录结构带进二进制；
# -w -s 去掉调试信息，镜像小一半
RUN go build \
      -trimpath \
      -ldflags "-w -s -X main.Version=${VERSION}" \
      -o /out/server \
      ./app/system/cmd/server

# 运行阶段
FROM gcr.io/distroless/static-debian12:nonroot

# 用 distroless 而不是 alpine：没有 shell 和包管理器，
# 攻击面小得多。CGO 已关闭，静态链接不需要 libc。
COPY --from=builder /out/server /app/server
COPY app/system/configs /app/configs

WORKDIR /app

# 8000 HTTP · 9000 gRPC · 9100 指标与健康检查
EXPOSE 8000 9000 9100

# 以非 root 运行，distroless 的 nonroot 变体已内置该用户
USER nonroot:nonroot

ENTRYPOINT ["/app/server"]
CMD ["-conf", "/app/configs"]

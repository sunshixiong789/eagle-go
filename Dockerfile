# 单体：一个进程、一个镜像。
FROM golang:1.27-alpine AS builder

ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0 GOOS=linux

WORKDIR /src
COPY . .

# BuildKit cache 保留模块与编译缓存。
# migrate/healthcheck 属于 tools module（它锁着生成工具链的版本，
# 不进业务依赖图），所以要用 `go -C tools` 单独构建；
# 普通 build 只会下载这两个命令真正 import 的依赖，不会拖进 buf/golangci-lint。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build \
      -trimpath \
      -ldflags "-w -s -X main.Version=${VERSION}" \
      -o /out/eagle \
      ./cmd/eagle && \
    go -C tools build -trimpath -ldflags "-w -s" -o /out/migrate ./migrate && \
    go -C tools build -trimpath -ldflags "-w -s" -o /out/healthcheck ./healthcheck && \
    cp -R migrations /out/migrations && \
    cp -R configs /out/configs

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION
LABEL org.opencontainers.image.title="eagle" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=builder /out/eagle /app/eagle
COPY --from=builder /out/migrate /app/migrate
COPY --from=builder /out/healthcheck /app/healthcheck
COPY --from=builder /out/migrations /app/migrations
COPY --from=builder /out/configs /app/configs

WORKDIR /app

# 容器内统一使用固定端口；宿主机端口由 Compose 映射。
ENV EAGLE_SERVER_HTTP_ADDR=0.0.0.0:8000 \
    EAGLE_OBSERVABILITY_METRICS_ADDR=0.0.0.0:9100
EXPOSE 8000 9100

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=12 \
  CMD ["/app/healthcheck", "-url", "http://127.0.0.1:9100/readyz"]

USER nonroot:nonroot
ENTRYPOINT ["/app/eagle"]
CMD ["-conf", "/app/configs"]

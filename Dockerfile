# syntax=docker/dockerfile:1

# ---------- 构建阶段：编译 + 运行全部测试 ----------
FROM golang:1.22-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 测试随构建一并运行（-race 覆盖并发隔离）：稳性求解、力臂扫描、单位换算、非法参数拦截。
RUN go vet ./... && go test -race ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/stabilityd ./cmd/server
# 预建数据目录并交给运行时的非 root 用户。
RUN mkdir -p /data && chown 65532:65532 /data

# ---------- 运行阶段：最小镜像，启动即提供服务 ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/stabilityd /stabilityd
COPY --from=build --chown=65532:65532 /data /data

ENV STABILITY_ADDR=:8080 \
    STABILITY_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/stabilityd"]

FROM golang:1.21-alpine AS builder

WORKDIR /app

# 复制go mod文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

ARG VERSION
ARG COMMIT
ARG BUILD_TIME

# 构建应用
RUN VERSION_VAL="${VERSION}" \
    && if [ -z "${VERSION_VAL}" ]; then VERSION_VAL="$(git describe --tags --exact-match 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo dev)"; fi \
    && COMMIT_VAL="${COMMIT}" \
    && if [ -z "${COMMIT_VAL}" ]; then COMMIT_VAL="$(git rev-parse --short HEAD 2>/dev/null || echo none)"; fi \
    && BUILD_TIME_VAL="${BUILD_TIME}" \
    && if [ -z "${BUILD_TIME_VAL}" ]; then BUILD_TIME_VAL="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; fi \
    && CGO_ENABLED=0 GOOS=linux go build -ldflags "-X xiaozhi/manager/backend/buildinfo.Version=${VERSION_VAL} -X xiaozhi/manager/backend/buildinfo.Commit=${COMMIT_VAL} -X xiaozhi/manager/backend/buildinfo.BuildTime=${BUILD_TIME_VAL}" -a -installsuffix cgo -o main .

# 运行阶段
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/

# 从构建阶段复制二进制文件
COPY --from=builder /app/main .
COPY --from=builder /app/config ./config

# 设置时区
ENV TZ=Asia/Shanghai

EXPOSE 8080

CMD ["./main"]

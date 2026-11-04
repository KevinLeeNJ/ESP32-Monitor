ARG GO_IMAGE=golang:1.26
FROM ${GO_IMAGE} AS build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /monitor-server ./cmd/monitor-server

FROM scratch
COPY --from=build /monitor-server /monitor-server
WORKDIR /state
ENTRYPOINT ["/monitor-server"]
CMD ["serve", "--config", "/state/monitor.yaml"]

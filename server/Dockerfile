ARG GO_IMAGE=golang:1.26
FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /monitor-server ./cmd/monitor-server && \
    mkdir /state && chown 65532:65532 /state

FROM scratch
COPY --from=build /monitor-server /monitor-server
COPY --from=build --chown=65532:65532 /state /state
WORKDIR /state
USER 65532:65532
ENTRYPOINT ["/monitor-server"]
CMD ["serve", "--config", "/state/monitor.yaml"]

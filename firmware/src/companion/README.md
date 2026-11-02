# Companion Protobuf

`companion.pb.c` and `companion.pb.h` are generated from the Client schema with
nanopb 0.4.9.1. The matching nanopb runtime and license are vendored under
`firmware/third_party/nanopb`. To regenerate in a Python environment containing
`nanopb==0.4.9.1` and `grpcio-tools==1.76.0`, from the repository root run:

```sh
python -m grpc_tools.protoc -I client/internal/transport/wire \
  --plugin=protoc-gen-nanopb="$(command -v protoc-gen-nanopb)" \
  --nanopb_out=-Iclient/internal/transport/wire:firmware/src/companion client/internal/transport/wire/companion.proto
```

Discovery strings have fixed limits in `companion.options`; other unbounded
strings, bytes and repeated fields use nanopb callbacks. A caller must
provide bounded storage and validate message semantics before applying data;
the generated decoder alone is not protocol validation.

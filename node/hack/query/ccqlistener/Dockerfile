# syntax=docker.io/docker/dockerfile:1.3@sha256:42399d4635eddd7a9b8a24be879d2f9a930d0ed040a61324cfdf59ef1357b3b2
FROM --platform=linux/amd64 docker.io/golang:1.23.3-bullseye@sha256:bc1b90c2a8eb0ffb62325e02a85d51031ad3afae15b3df4b6a48b7929b00badb AS build
# libwasmvm.so is not compatible with arm

WORKDIR /app

COPY node/tools tools

RUN --mount=type=cache,target=/root/.cache --mount=type=cache,target=/go \
  cd tools/ && go build -mod=readonly -o /dlv github.com/go-delve/delve/cmd/dlv

COPY node node
COPY sdk sdk
COPY wormchain wormchain

ARG GO_BUILD_ARGS=-race

RUN --mount=type=cache,target=/root/.cache --mount=type=cache,target=/go \
  export CGO_ENABLED=1 && \
  cd node/hack/query/ccqlistener && \
  go build ${GO_BUILD_ARGS} -gcflags="all=-N -l" --ldflags '-X "github.com/certusone/wormhole/node/cmd/guardiand.Build=dev"' -mod=readonly -o /ccqlistener ccqlistener.go && \
  go get github.com/CosmWasm/wasmvm@v1.1.1 && \
  cp /go/pkg/mod/github.com/!cosm!wasm/wasmvm@v1.1.1/internal/api/libwasmvm.x86_64.so /usr/lib/

# Only export the final binary (+ shared objects). This reduces the image size
# from ~1GB to ~150MB.
FROM scratch as export

# guardiand can't (easily) be statically linked due to the C dependencies, so we
# have to copy all the dynamic libraries
COPY --from=build /lib/* /lib/
COPY --from=build /lib64/* /lib64/
COPY --from=build /usr/lib/libwasmvm.x86_64.so /usr/lib/

# Copy the shells as entrypoints, but no utilities are necessary
COPY --from=build /bin/bash /bin/dash /bin/sh /bin/

# finally copy the guardian executable
COPY --from=build /ccqlistener .

CMD ["/ccqlistener", "--configDir /app/cfg"]

# Build from the repo root: docker build -t tallymoth .
# Runs on the build machine and cross-compiles, so multi-arch images need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG VERSION=dev TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /tallymoth ./cmd/tallymoth && mkdir /data

FROM scratch
COPY --from=build /tallymoth /tallymoth
# empty data dir owned by the runtime user, so a fresh named volume is writable
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
EXPOSE 8080 8081
# scratch has no curl: the binary checks itself
HEALTHCHECK --interval=30s --timeout=3s CMD ["/tallymoth", "healthcheck"]
ENTRYPOINT ["/tallymoth"]
CMD ["serve", "--config", "/config/tallymoth.yaml"]

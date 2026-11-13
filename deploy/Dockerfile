# Build from the repo root: docker build -f deploy/Dockerfile -t tallymoth .
FROM golang:1.27 AS build
ARG VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /tallymoth ./cmd/tallymoth

FROM scratch
COPY --from=build /tallymoth /tallymoth
USER 65532:65532
EXPOSE 8080 8081
# scratch has no curl: the binary checks itself
HEALTHCHECK --interval=30s --timeout=3s CMD ["/tallymoth", "healthcheck"]
ENTRYPOINT ["/tallymoth"]
CMD ["serve", "--config", "/config/tallymoth.yaml"]

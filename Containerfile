# Run `make image` (or build bin/hoshilet-server for Linux with CGO_ENABLED=0) first.
FROM docker.io/library/debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --no-create-home hoshilet
COPY bin/hoshilet-server /usr/local/bin/hoshilet-server
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/hoshilet-server"]

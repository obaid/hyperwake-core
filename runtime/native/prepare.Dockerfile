FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends e2fsprogs zstd ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY prepare-guest.sh /usr/local/bin/prepare-guest
ENTRYPOINT ["bash", "/usr/local/bin/prepare-guest"]

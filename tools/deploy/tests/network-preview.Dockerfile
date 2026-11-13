# Reuse the deploy harness's systemd host; no application or database install.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
RUN apt-get update -qq \
    && apt-get install -y --no-install-recommends nginx libnginx-mod-stream ufw iptables nftables \
    && rm -rf /var/lib/apt/lists/*

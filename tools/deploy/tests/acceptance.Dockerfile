# Keep F0's systemd image as the single owner of base host setup. Install probe
# clients before any foreign baseline, so acceptance itself cannot restart a
# foreign service while preparing backup tools. No database server is installed.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
RUN apt-get update -qq \
    && apt-get install -y --no-install-recommends age rclone pgbackrest \
    && rm -rf /var/lib/apt/lists/*

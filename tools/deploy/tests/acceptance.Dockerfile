# Keep F0's systemd image as the single owner of base host setup. Install probe
# clients before any foreign baseline, so acceptance itself cannot restart a
# foreign service while preparing backup tools. No database server is installed.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
RUN apt-get update -qq \
    && apt-get install -y --no-install-recommends age rclone pgbackrest e2fsprogs util-linux \
    && rm -rf /var/lib/apt/lists/*
# Some Ubuntu image builds provide a package-autostart policy. Retain it as
# evidence outside the active path before this image becomes a fixture host.
# Production still refuses an existing policy-rc.d rather than replacing it.
RUN if [ -e /usr/sbin/policy-rc.d ] || [ -L /usr/sbin/policy-rc.d ]; then \
      mv /usr/sbin/policy-rc.d /usr/sbin/policy-rc.d.acceptance-image; \
    fi

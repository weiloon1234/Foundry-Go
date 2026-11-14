ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION}
ENV DEBIAN_FRONTEND=noninteractive
# Model a maintained server: the deploy tool never upgrades installed packages,
# so an image older than the current archive would refuse later package plans.
# dbus matches real Ubuntu servers: unprivileged systemctl (the public host report) needs the system bus.
RUN apt-get update -qq && apt-get -y upgrade && apt-get install -y --no-install-recommends systemd systemd-sysv dbus python3 sudo iproute2 procps openssl ca-certificates && rm -rf /var/lib/apt/lists/*
ENV container=docker
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

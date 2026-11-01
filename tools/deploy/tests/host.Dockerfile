ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -y --no-install-recommends systemd systemd-sysv python3 sudo iproute2 procps openssl ca-certificates && rm -rf /var/lib/apt/lists/*
ENV container=docker
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

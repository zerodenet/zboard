ARG BASE_IMAGE=debian:trixie-slim
FROM ${BASE_IMAGE}
ENV container=docker
RUN rm -f /etc/apt/sources.list /etc/apt/sources.list.d/* \
 && printf '%s\n' 'deb https://deb.debian.org/debian trixie main' > /etc/apt/sources.list \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd systemd-sysv openssh-server ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /root/.ssh /etc/zerodenet /var/lib/zerodenet \
 && chmod 0700 /root/.ssh \
 && systemctl enable ssh.service
COPY zero /usr/local/bin/zero
COPY sshd_config /etc/ssh/sshd_config
COPY zero.service /etc/systemd/system/zero.service
RUN systemctl mask systemd-modules-load.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

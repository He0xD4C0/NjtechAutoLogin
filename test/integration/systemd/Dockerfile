FROM golang:1.24-alpine AS build
WORKDIR /src
COPY src/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags "-X main.version=2.0.0-test" -o /njtechlogin .

FROM debian:bookworm-slim
ENV container=docker
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd dbus \
 && rm -rf /var/lib/apt/lists/* \
 && rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/sysinit.target.wants/systemd-tmpfiles-setup* \
          /lib/systemd/system/systemd-update-utmp*
COPY --from=build /njtechlogin /opt/njtechlogin-test/njtechlogin
COPY test/integration/systemd/smoke.sh /opt/njtechlogin-test/smoke.sh
RUN chmod 0755 /opt/njtechlogin-test/njtechlogin /opt/njtechlogin-test/smoke.sh
STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]

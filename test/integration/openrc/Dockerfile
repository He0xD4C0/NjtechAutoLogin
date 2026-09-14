FROM golang:1.24-alpine AS build
WORKDIR /src
COPY src/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags "-X main.version=2.0.0-test" -o /njtechlogin .

FROM alpine:3.22
RUN apk add --no-cache openrc
COPY test/integration/openrc/syslog /etc/init.d/syslog
RUN chmod 0755 /etc/init.d/syslog \
 && rc-update add syslog boot
COPY --from=build /njtechlogin /opt/njtechlogin-test/njtechlogin
COPY test/integration/openrc-smoke.sh /opt/njtechlogin-test/smoke.sh
RUN chmod 0755 /opt/njtechlogin-test/njtechlogin /opt/njtechlogin-test/smoke.sh
STOPSIGNAL SIGTERM
CMD ["/sbin/init"]

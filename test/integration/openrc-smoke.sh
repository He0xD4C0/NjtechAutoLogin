#!/bin/sh
set -eu

test "$(cat /proc/1/comm)" = "init"
test "$(rc-status --runlevel)" = "default"

binary=/opt/njtechlogin-test/njtechlogin
printf '%s\n' '*' | "$binary" install --username '*' --provider telecom --password-stdin
sleep 1
"$binary" status
rc-service njtechlogin status
rc-update show default | grep -q '[[:space:]]njtechlogin[[:space:]]'
grep -q 'njtechlogin' /var/log/messages

old_pid="$(pidof njtechlogin)"
test -n "$old_pid"
kill -KILL "$old_pid"
sleep 7
new_pid="$(pidof njtechlogin)"
test -n "$new_pid"
test "$new_pid" != "$old_pid"

"$binary" stop
if "$binary" status; then
	echo "status unexpectedly reported running after stop" >&2
	exit 1
else
	test "$?" -eq 3
fi
test -z "$(pidof njtechlogin || true)"

"$binary" start
sleep 1
"$binary" status
"$binary" uninstall --purge
test ! -e /etc/init.d/njtechlogin
test ! -e /usr/local/bin/njtechlogin
test ! -e /etc/njtechlogin/config.yml
test ! -e /etc/njtechlogin

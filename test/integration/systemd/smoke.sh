#!/bin/sh
set -eu

binary=/opt/njtechlogin-test/njtechlogin
printf '%s\n' '*' | "$binary" install --username '*' --provider telecom --password-stdin
"$binary" status
systemd-analyze verify /etc/systemd/system/njtechlogin.service

old_pid="$(systemctl show -p MainPID --value njtechlogin)"
test "$old_pid" -gt 1
kill -KILL "$old_pid"
sleep 7
new_pid="$(systemctl show -p MainPID --value njtechlogin)"
test "$new_pid" -gt 1
test "$new_pid" != "$old_pid"

"$binary" stop
if "$binary" status; then
	echo "status unexpectedly reported running after stop" >&2
	exit 1
else
	test "$?" -eq 3
fi
"$binary" start
sleep 1
"$binary" status
journalctl -u njtechlogin --no-pager -n 12

"$binary" uninstall --purge
test ! -e /etc/systemd/system/njtechlogin.service
test ! -e /usr/local/bin/njtechlogin
test ! -e /etc/njtechlogin

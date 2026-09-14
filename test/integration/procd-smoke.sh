#!/bin/sh
set -eu

fixture_dir="${NJTECH_FIXTURE_DIR:-/opt/njtechlogin-test/procd}"
binary="${NJTECH_TEST_BINARY:-/opt/njtechlogin-test/njtechlogin}"

cp "$fixture_dir/openwrt_release" /etc/openwrt_release
cp "$fixture_dir/rc.common" /etc/rc.common
chmod 0755 /etc/rc.common

printf '%s\n' '*' | "$binary" install --username '*' --provider telecom --password-stdin
"$binary" status
test -e /tmp/njtechlogin-enabled
test -e /tmp/njtechlogin-procd.pid
grep -q 'USE_PROCD=1' /etc/init.d/njtechlogin
grep -q 'procd_set_param respawn 60 5 5' /etc/init.d/njtechlogin

"$binary" stop
test ! -e /tmp/njtechlogin-procd.pid
if "$binary" status; then
	echo "status unexpectedly reported running after stop" >&2
	exit 1
else
	test "$?" -eq 3
fi
"$binary" start
"$binary" status
"$binary" uninstall --purge
test ! -e /etc/init.d/njtechlogin
test ! -e /usr/bin/njtechlogin
test ! -e /etc/njtechlogin

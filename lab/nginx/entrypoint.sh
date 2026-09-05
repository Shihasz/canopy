#!/bin/sh
set -e
mkdir -p /run/sshd
/usr/sbin/sshd
exec nginx -g "daemon off;"

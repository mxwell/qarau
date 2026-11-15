#!/bin/sh
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
if [ "$1" = purge ]; then
	rm -rf /var/lib/qarau-fetch /var/log/qarau-fetch
fi

#!/bin/sh
set -e

if [ "$1" = remove ]; then
	if [ -d /run/systemd/system ]; then
		systemctl stop qarau-fetch.service || true
	fi
	systemctl disable qarau-fetch.service >/dev/null || true
fi

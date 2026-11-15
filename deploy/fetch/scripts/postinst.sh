#!/bin/sh
set -e

if [ "$1" = configure ]; then
	if ! getent passwd qarau >/dev/null; then
		adduser --system --group --no-create-home --home /var/lib/qarau-fetch \
			--shell /usr/sbin/nologin qarau
	fi
	chgrp qarau /etc/qarau-fetch/fetch.env /etc/qarau-fetch/certs

	systemctl enable qarau-fetch.service >/dev/null
	if [ -d /run/systemd/system ]; then
		systemctl daemon-reload
		if [ -n "$2" ]; then
			systemctl try-restart qarau-fetch.service
		else
			echo "qarau-fetch: edit /etc/qarau-fetch/fetch.env, put ca.crt, fetch.crt, fetch.key"
			echo "into /etc/qarau-fetch/certs/, then run: systemctl start qarau-fetch"
		fi
	fi
fi

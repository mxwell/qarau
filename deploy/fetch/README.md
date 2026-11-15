# Fetch worker as a Debian package

Packages `fetch` as an arm64 `.deb` with a systemd service, for Debian 12 on Raspberry Pi.

## Build

```sh
make tools        # once, installs nfpm
make deb-fetch    # -> bin/qarau-fetch.deb
```

The version defaults to `0.0.<commit count>+g<short hash>`; override with `DEB_VERSION=...`.

## Install

yt-dlp is not a package dependency (Debian's own is too old). Install it outside `/home`,
since the service runs as the `qarau` user and cannot reach your home directory:

```sh
sudo python3 -m venv /opt/yt-dlp
sudo /opt/yt-dlp/bin/pip install yt-dlp
```

Same goes for the optional JS runtime (e.g. deno): put it under `/usr/local/bin` or `/opt`.

Install the package, fill in the config and the client certs (see
[mTLS](../../README.md#mtls-generating-the-ca-and-certs)), then start the service:

```sh
sudo apt install ./qarau-fetch.deb
sudoedit /etc/qarau-fetch/fetch.env      # QARAU_API_HOST, QARAU_TOOL=/opt/yt-dlp/bin/yt-dlp, ...
sudo install -m 640 -g qarau ca.crt fetch.crt fetch.key /etc/qarau-fetch/certs/
sudo systemctl start qarau-fetch
```

The service is enabled on install, so it starts on boot. Upgrades (`apt install` of a newer
`.deb`) restart a running service and keep your edits to `fetch.env`.

## Operate

```sh
systemctl status qarau-fetch
journalctl -u qarau-fetch -f             # startup errors
tail -f /var/log/qarau-fetch/*.jsonl     # application logs
sudo systemctl restart qarau-fetch       # after editing fetch.env
sudo /opt/yt-dlp/bin/pip install -U yt-dlp
```

The service is restarted on failure.

## Files

| Path                                  | What                                       |
|---------------------------------------|--------------------------------------------|
| `/usr/bin/qarau-fetch`                | binary                                     |
| `/lib/systemd/system/qarau-fetch.service` | unit                                   |
| `/etc/qarau-fetch/fetch.env`          | config, overrides defaults set in the unit |
| `/etc/qarau-fetch/certs/`             | `ca.crt`, `fetch.crt`, `fetch.key`         |
| `/var/lib/qarau-fetch/work`           | per-job download dirs                      |
| `/var/log/qarau-fetch`                | daily JSONL logs                           |

`apt remove` stops and disables the service; `apt purge` also deletes `/var/lib/qarau-fetch`
and `/var/log/qarau-fetch`, but keeps the certs.

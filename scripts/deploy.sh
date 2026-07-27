#!/usr/bin/env bash
# Ship the website binary to the deploy host and restart it.
# Invoke via `make deploy`, which builds and runs the test gate first.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

DEPLOY_HOST="${DEPLOY_HOST:-10.30.1.244}"
DEPLOY_USER="${DEPLOY_USER:-ec2-user}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/pm-prod.pem}"
SSH_OPTS=(-i "$SSH_KEY" -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)

die() { echo "ERROR: $*" >&2; exit 1; }

[ -f bin/hushield-web ] || die "bin/hushield-web missing. Run 'make build-linux-arm64'."
file bin/hushield-web | grep -q 'ARM aarch64' || die "bin/hushield-web is not ARM aarch64."

echo "==> Uploading to ${DEPLOY_USER}@${DEPLOY_HOST}"
scp "${SSH_OPTS[@]}" bin/hushield-web "${DEPLOY_USER}@${DEPLOY_HOST}:/tmp/" >/dev/null

echo "==> Installing and restarting"
ssh "${SSH_OPTS[@]}" "${DEPLOY_USER}@${DEPLOY_HOST}" 'sudo bash -s' <<'REMOTE'
set -euo pipefail
install -o hushield-web -g hushield-web -m 0755 /tmp/hushield-web /opt/hushield-web/bin/hushield-web
rm -f /tmp/hushield-web
systemctl restart hushield-web
sleep 3
if ! systemctl is-active --quiet hushield-web; then
	journalctl -u hushield-web -n 30 --no-pager >&2
	exit 1
fi
curl -fsS --max-time 10 http://127.0.0.1:8081/healthz >/dev/null
echo "    hushield-web active, /healthz OK"
REMOTE
echo "==> Deploy complete"

from __future__ import annotations

import sys
from pathlib import Path

import paramiko

CFG_PATH = Path(__file__).with_name("server.env")

SETUP_CMD = r"""
set -e

cat >/usr/local/sbin/disk-alert.sh <<'EOF'
#!/bin/bash
THRESHOLD="${DISK_ALERT_THRESHOLD:-80}"
MOUNT="${DISK_ALERT_MOUNT:-/}"
USE=$(df -P "$MOUNT" | awk 'NR==2 {gsub(/%/,"",$5); print $5}')
if [ -z "$USE" ]; then
  logger -t disk-alert "unable to read disk usage for $MOUNT"
  exit 1
fi
if [ "$USE" -ge "$THRESHOLD" ]; then
  msg="disk usage ${USE}% on ${MOUNT} >= ${THRESHOLD}%"
  logger -t disk-alert "$msg"
  echo "$(date '+%F %T') $msg" >> /var/log/disk-alert.log
fi
EOF
chmod 755 /usr/local/sbin/disk-alert.sh

cat >/usr/local/sbin/resource-check.sh <<'EOF'
#!/bin/bash
{
  echo "===== $(date '+%F %T %Z') ====="
  df -h /
  echo
  free -h
  echo
  docker compose -f /opt/aigc-3d-platform/docker-compose.yml --project-directory /opt/aigc-3d-platform ps || true
} >> /var/log/resource-check.log 2>&1
tail -n 2000 /var/log/resource-check.log > /var/log/resource-check.log.tmp || true
mv /var/log/resource-check.log.tmp /var/log/resource-check.log
EOF
chmod 755 /usr/local/sbin/resource-check.sh

cat >/etc/cron.d/disk-alert <<'EOF'
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin
0 * * * * root /usr/local/sbin/disk-alert.sh
15 3 * * * root /usr/local/sbin/resource-check.sh
EOF
chmod 644 /etc/cron.d/disk-alert
touch /var/log/disk-alert.log /var/log/resource-check.log
chmod 640 /var/log/disk-alert.log /var/log/resource-check.log

/usr/local/sbin/disk-alert.sh
/usr/local/sbin/resource-check.sh

if ! command -v firewall-cmd >/dev/null 2>&1; then
  if command -v dnf >/dev/null 2>&1; then
    dnf install -y firewalld
  else
    yum install -y firewalld
  fi
fi

systemctl start firewalld
firewall-cmd --permanent --zone=public --add-service=ssh
firewall-cmd --permanent --zone=public --add-service=http
firewall-cmd --permanent --zone=public --remove-service=dhcpv6-client || true
firewall-cmd --reload
systemctl enable firewalld
systemctl restart docker
sleep 5
systemctl is-active docker
systemctl is-active firewalld

cd /opt/aigc-3d-platform
docker compose up -d --remove-orphans --wait --wait-timeout 180
echo PHASE2_SETUP_DONE
"""

VERIFY_CMD = r"""
set +e
echo '===== FIREWALL ====='
systemctl is-enabled firewalld
systemctl is-active firewalld
firewall-cmd --state
firewall-cmd --list-all
echo '===== LISTEN ====='
ss -lnt | grep -E ':22|:80|:3306|:6379|:9000|:9001|:8080|:5173' || true
echo '===== OVERCOMMIT ====='
sysctl vm.overcommit_memory
echo '===== DOCKER LOG ====='
python3 - <<'PY'
import json
from pathlib import Path
data = json.loads(Path('/etc/docker/daemon.json').read_text(encoding='utf-8'))
print('log-driver=', data.get('log-driver'))
print('log-opts=', data.get('log-opts'))
PY
echo '===== DIRS ====='
ls -ld /opt/aigc-3d-platform /opt/backup
ls -lt /opt/backup | head
echo '===== TIME ====='
timedatectl | sed -n '1,8p'
echo '===== DISK ALERT ====='
ls -l /usr/local/sbin/disk-alert.sh /usr/local/sbin/resource-check.sh /etc/cron.d/disk-alert
cat /etc/cron.d/disk-alert
echo '===== CONTAINERS ====='
docker compose -f /opt/aigc-3d-platform/docker-compose.yml --project-directory /opt/aigc-3d-platform ps
echo '===== LOCAL HEALTH ====='
for u in http://127.0.0.1/ http://127.0.0.1/healthz http://127.0.0.1/readyz http://127.0.0.1/api/v1/version
do
  code=$(curl -sS -o /tmp/hc_body -w '%{http_code}' --max-time 8 "$u")
  echo "URL=$u CODE=$code"
done
echo '===== PUBLIC ====='
curl -sS -o /tmp/pub -w 'PUBLIC_ROOT=%{http_code}\n' --max-time 8 http://127.0.0.1/
curl -sS -o /tmp/pub -w 'PUBLIC_HEALTHZ=%{http_code}\n' --max-time 8 http://8.154.28.98/healthz
echo '===== DONE ====='
"""


def load_cfg() -> dict[str, str]:
    cfg: dict[str, str] = {}
    for raw in CFG_PATH.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        cfg[key.strip()] = value.strip()
    return cfg


def connect(cfg: dict[str, str]) -> paramiko.SSHClient:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(
        hostname=cfg["SSH_HOST"],
        port=int(cfg.get("SSH_PORT", "22")),
        username=cfg["SSH_USER"],
        password=cfg["SSH_PASSWORD"],
        timeout=20,
        allow_agent=False,
        look_for_keys=False,
        banner_timeout=20,
        auth_timeout=20,
    )
    return client


def run(client: paramiko.SSHClient, command: str, timeout: int = 300) -> tuple[int, str]:
    stdin, stdout, stderr = client.exec_command(command, timeout=timeout, get_pty=True)
    out = stdout.read().decode("utf-8", errors="replace")
    err = stderr.read().decode("utf-8", errors="replace")
    code = stdout.channel.recv_exit_status()
    return code, out + (("\n" + err) if err.strip() else "")


def main() -> None:
    stage = sys.argv[1] if len(sys.argv) > 1 else "all"
    cfg = load_cfg()
    client = connect(cfg)
    try:
        if stage in {"setup", "all"}:
            print("=== setup remaining phase 2 baseline ===")
            code, out = run(client, SETUP_CMD, timeout=300)
            sys.stdout.buffer.write(out.encode(sys.stdout.encoding or "utf-8", errors="replace"))
            if code != 0:
                raise SystemExit(f"setup failed ({code})")
        if stage in {"verify", "all"}:
            print("=== verify phase 2 ===")
            code, out = run(client, VERIFY_CMD, timeout=120)
            sys.stdout.buffer.write(out.encode(sys.stdout.encoding or "utf-8", errors="replace"))
            if code != 0:
                raise SystemExit(f"verify failed ({code})")
    finally:
        client.close()


if __name__ == "__main__":
    main()

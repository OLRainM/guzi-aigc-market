from __future__ import annotations

import sys
from pathlib import Path

import paramiko

CFG_PATH = Path(__file__).with_name("server.env")

CHECKS = r"""
set +e
echo '===== OS ====='
cat /etc/os-release | sed -n '1,6p'
uname -a
echo '===== DOCKER ====='
systemctl is-enabled docker
systemctl is-active docker
docker --version
docker compose version
docker buildx version | head -n 1
git --version
curl --version | head -n 1
openssl version
echo '===== DOCKER GROUP ====='
id deploy
getent group docker
echo '===== SYSCTL ====='
sysctl vm.overcommit_memory
ls -l /etc/sysctl.d/99-redis-overcommit.conf 2>/dev/null || echo NO_OVERCOMMIT_CONF
cat /etc/sysctl.d/99-redis-overcommit.conf 2>/dev/null || true
echo '===== DOCKER DAEMON ====='
cat /etc/docker/daemon.json 2>/dev/null || echo NO_DAEMON_JSON
echo '===== FIREWALL ====='
systemctl is-enabled firewalld 2>/dev/null || echo firewalld_not_enabled
systemctl is-active firewalld 2>/dev/null || echo firewalld_not_active
command -v firewall-cmd >/dev/null && firewall-cmd --state || echo no_firewall_cmd
command -v firewall-cmd >/dev/null && firewall-cmd --list-all || true
echo '===== LISTEN ====='
ss -lntup | sed -n '1,80p'
echo '===== DIRS ====='
ls -ld /opt/aigc-3d-platform /opt/backup 2>/dev/null || true
ls -lt /opt/backup 2>/dev/null | head
echo '===== TIME ====='
timedatectl
chronyc tracking 2>/dev/null | head -n 8 || true
systemctl is-enabled chronyd 2>/dev/null || systemctl is-enabled chrony 2>/dev/null || echo chrony_not_enabled
systemctl is-active chronyd 2>/dev/null || systemctl is-active chrony 2>/dev/null || echo chrony_not_active
echo '===== DISK/MEM ====='
df -h /
free -h
echo '===== DISK ALERT ====='
ls -l /usr/local/sbin/disk-alert.sh /etc/cron.d/disk-alert 2>/dev/null || echo NO_DISK_ALERT
cat /etc/cron.d/disk-alert 2>/dev/null || true
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


def main() -> None:
    cfg = load_cfg()
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
    try:
        stdin, stdout, stderr = client.exec_command(CHECKS, timeout=60, get_pty=True)
        out = stdout.read().decode("utf-8", errors="replace")
        err = stderr.read().decode("utf-8", errors="replace")
        sys.stdout.buffer.write(out.encode(sys.stdout.encoding or "utf-8", errors="replace"))
        if err.strip():
            sys.stderr.buffer.write(err.encode(sys.stderr.encoding or "utf-8", errors="replace"))
        raise SystemExit(stdout.channel.recv_exit_status())
    finally:
        client.close()


if __name__ == "__main__":
    main()

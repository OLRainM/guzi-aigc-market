from __future__ import annotations

import posixpath
import sys
import time
from pathlib import Path

import paramiko

ROOT = Path(__file__).resolve().parents[1]
CFG_PATH = Path(__file__).with_name("server.env")
REMOTE_DIR = "/opt/aigc-3d-platform"
PUBLIC_ORIGIN = "http://8.154.28.98"

SKIP_DIRS = {
    ".git",
    "node_modules",
    "dist",
    "__pycache__",
    ".venv",
    "venv",
    ".pytest_cache",
    ".mypy_cache",
    ".codebuddy",
    ".deploy",
}
SKIP_FILES = {".env", "server.env"}
UPLOAD_ROOTS = [
    ROOT / "docker-compose.yml",
    ROOT / "apps" / "api",
    ROOT / "apps" / "web",
    ROOT / "apps" / "worker",
]


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


def run(client: paramiko.SSHClient, command: str, timeout: int = 120) -> tuple[int, str]:
    stdin, stdout, stderr = client.exec_command(command, timeout=timeout, get_pty=True)
    channel = stdout.channel
    channel.settimeout(30)
    chunks: list[str] = []
    started = time.time()
    last_log = started
    while True:
        if time.time() - started > timeout:
            channel.close()
            raise TimeoutError(f"remote command exceeded {timeout}s")
        got = False
        try:
            if channel.recv_ready():
                chunks.append(channel.recv(32768).decode("utf-8", errors="replace"))
                got = True
            if channel.recv_stderr_ready():
                chunks.append(channel.recv_stderr(32768).decode("utf-8", errors="replace"))
                got = True
        except Exception:
            pass
        if channel.exit_status_ready():
            while channel.recv_ready():
                chunks.append(channel.recv(32768).decode("utf-8", errors="replace"))
            while channel.recv_stderr_ready():
                chunks.append(channel.recv_stderr(32768).decode("utf-8", errors="replace"))
            break
        now = time.time()
        if now - last_log > 20:
            print(f"  ... still running ({int(now - started)}s)")
            last_log = now
        if not got:
            time.sleep(0.3)
    code = channel.recv_exit_status()
    return code, "".join(chunks)


def safe_print(text: str) -> None:
    encoding = sys.stdout.encoding or "utf-8"
    sys.stdout.buffer.write((text + "\n").encode(encoding, errors="replace"))
    sys.stdout.flush()


def must_run(client: paramiko.SSHClient, command: str, timeout: int = 120) -> str:
    safe_print(f"\n$ {command[:180]}")
    code, out = run(client, command, timeout=timeout)
    if out.strip():
        safe_print(out[-8000:])
    if code != 0:
        raise SystemExit(f"command failed ({code}): {command}")
    return out


def iter_local_files() -> list[tuple[Path, str]]:
    files: list[tuple[Path, str]] = []
    for item in UPLOAD_ROOTS:
        if item.is_file():
            files.append((item, posixpath.join(REMOTE_DIR, item.name)))
            continue
        for path in item.rglob("*"):
            if not path.is_file():
                continue
            if any(part in SKIP_DIRS for part in path.parts):
                continue
            if path.name in SKIP_FILES:
                continue
            rel = path.relative_to(ROOT).as_posix()
            files.append((path, posixpath.join(REMOTE_DIR, rel)))
    return files


def ensure_remote_dir(sftp: paramiko.SFTPClient, directory: str) -> None:
    parts = [p for p in directory.split("/") if p]
    current = ""
    for part in parts:
        current += "/" + part
        try:
            sftp.stat(current)
        except FileNotFoundError:
            sftp.mkdir(current)


def upload(client: paramiko.SSHClient) -> None:
    sftp = client.open_sftp()
    files = iter_local_files()
    print(f"uploading {len(files)} files")
    for index, (local, remote) in enumerate(files, 1):
        ensure_remote_dir(sftp, posixpath.dirname(remote))
        sftp.put(str(local), remote)
        if index == 1 or index % 20 == 0 or index == len(files):
            print(f"  {index}/{len(files)} {remote}")
    sftp.close()


PREPARE_CMD = r"""
set -e
STAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p /opt/backup
if [ -d /opt/aigc-3d-platform ]; then
  tar --exclude='./.git' -czf /opt/backup/aigc-3d-platform-$STAMP.tar.gz -C /opt aigc-3d-platform
fi
echo BACKUP=/opt/backup/aigc-3d-platform-$STAMP.tar.gz
systemctl stop nginx || true
systemctl disable nginx || true
systemctl is-active nginx || true
ss -lnt | grep ':80' || echo PORT80_FREE
python3 - <<'PY'
from pathlib import Path
path = Path('/opt/aigc-3d-platform/.env')
text = path.read_text(encoding='utf-8')
updates = {
    'WEB_PORT': '80',
    'COOKIE_SECURE': 'false',
    'CORS_ALLOW_ORIGIN': 'http://8.154.28.98',
    'VITE_API_BASE_URL': '',
    'MYSQL_PORT': '3306',
    'REDIS_PORT': '6379',
    'MINIO_PORT': '9000',
    'MINIO_CONSOLE_PORT': '9001',
    'API_PORT': '8080',
}
lines = []
seen = set()
for raw in text.splitlines():
    if not raw.strip() or raw.lstrip().startswith('#') or '=' not in raw:
        lines.append(raw)
        continue
    key, _ = raw.split('=', 1)
    key = key.strip()
    if key in updates:
        lines.append(f'{key}={updates[key]}')
        seen.add(key)
    else:
        lines.append(raw)
for key, value in updates.items():
    if key not in seen:
        lines.append(f'{key}={value}')
path.write_text('\n'.join(lines) + '\n', encoding='utf-8')
path.chmod(0o640)
print('ENV_PATCHED')
print('ENV_KEYS')
for raw in path.read_text(encoding='utf-8').splitlines():
    if not raw.strip() or raw.lstrip().startswith('#') or '=' not in raw:
        continue
    key, val = raw.split('=', 1)
    key = key.strip()
    if any(token in key for token in ('PASSWORD', 'SECRET', 'DSN', 'TOKEN', 'KEY')):
        print(f'{key}=***REDACTED*** len={len(val)}')
    else:
        print(f'{key}={val}')
PY
cat >/etc/sysctl.d/99-redis-overcommit.conf <<'EOF'
vm.overcommit_memory = 1
EOF
sysctl -w vm.overcommit_memory=1
python3 - <<'PY'
import json
from pathlib import Path
path = Path('/etc/docker/daemon.json')
data = {}
if path.exists() and path.read_text(encoding='utf-8').strip():
    data = json.loads(path.read_text(encoding='utf-8'))
data['log-driver'] = 'json-file'
data['log-opts'] = {'max-size': '10m', 'max-file': '3'}
path.write_text(json.dumps(data, indent=4) + '\n', encoding='utf-8')
print('DAEMON_JSON_UPDATED')
PY
systemctl restart docker
sleep 3
systemctl is-active docker
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
"""

BUILD_CMD = r"""
set -e
cd /opt/aigc-3d-platform
chown -R deploy:deploy /opt/aigc-3d-platform
chmod 640 /opt/aigc-3d-platform/.env
docker compose config -q
docker compose build api worker web
docker compose up -d --remove-orphans --wait --wait-timeout 180
docker compose ps
"""

VERIFY_CMD = r"""
set +e
echo '===== CONTAINERS ====='
docker compose -f /opt/aigc-3d-platform/docker-compose.yml --project-directory /opt/aigc-3d-platform ps
echo '===== LISTEN 80 ====='
ss -lntup | grep -E ':80|:443|:5173|:8080' || true
echo '===== LOCAL HEALTH ====='
for u in \
  http://127.0.0.1/ \
  http://127.0.0.1/healthz \
  http://127.0.0.1/readyz \
  http://127.0.0.1/api/v1/version
 do
  code=$(curl -sS -o /tmp/hc_body -w '%{http_code}' --max-time 8 "$u")
  echo "URL=$u CODE=$code"
  head -c 180 /tmp/hc_body; echo
 done
echo '===== COOKIE CHECK ====='
USER_NAME="deploycheck831"
PASS="DeployCheck831!"
reg=$(curl -sS -D /tmp/reg_hdr -o /tmp/reg_body -w '%{http_code}' --max-time 15 \
  -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1/api/v1/auth/register \
  -d "{\"username\":\"$USER_NAME\",\"email\":\"deploycheck831@example.com\",\"password\":\"$PASS\"}")
echo REGISTER_CODE=$reg
grep -i 'set-cookie' /tmp/reg_hdr || true
head -c 180 /tmp/reg_body; echo
if [ "$reg" != "201" ] && [ "$reg" != "409" ]; then
  echo REGISTER_UNEXPECTED
fi
login=$(curl -sS -D /tmp/login_hdr -o /tmp/login_body -c /tmp/cj -w '%{http_code}' --max-time 15 \
  -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1/api/v1/auth/login \
  -d "{\"identifier\":\"$USER_NAME\",\"password\":\"$PASS\"}")
echo LOGIN_CODE=$login
grep -i 'set-cookie' /tmp/login_hdr || true
head -c 180 /tmp/login_body; echo
refresh=$(curl -sS -D /tmp/ref_hdr -o /tmp/ref_body -b /tmp/cj -c /tmp/cj -w '%{http_code}' --max-time 15 \
  -X POST http://127.0.0.1/api/v1/auth/refresh)
echo REFRESH_CODE=$refresh
grep -i 'set-cookie' /tmp/ref_hdr || true
head -c 180 /tmp/ref_body; echo
logout=$(curl -sS -D /tmp/lo_hdr -o /tmp/lo_body -b /tmp/cj -w '%{http_code}' --max-time 15 \
  -X POST http://127.0.0.1/api/v1/auth/logout)
echo LOGOUT_CODE=$logout
echo '===== PUBLIC ====='
curl -sS -o /tmp/pub -w 'PUBLIC_ROOT=%{http_code}\n' --max-time 8 http://8.154.28.98/
head -c 120 /tmp/pub; echo
curl -sS -o /tmp/pub -w 'PUBLIC_HEALTHZ=%{http_code}\n' --max-time 8 http://8.154.28.98/healthz
head -c 120 /tmp/pub; echo
curl -sS -o /tmp/pub -w 'PUBLIC_READYZ=%{http_code}\n' --max-time 8 http://8.154.28.98/readyz
head -c 120 /tmp/pub; echo
curl -sS -o /tmp/pub -w 'PUBLIC_VERSION=%{http_code}\n' --max-time 8 http://8.154.28.98/api/v1/version
head -c 120 /tmp/pub; echo
echo '===== SYSCTL ====='
sysctl vm.overcommit_memory
echo '===== DONE ====='
"""


def main() -> None:
    stage = sys.argv[1] if len(sys.argv) > 1 else "all"
    allowed = {"prepare", "upload", "build", "verify", "status", "all"}
    if stage not in allowed:
        raise SystemExit(f"usage: sync_and_deploy.py [{' | '.join(sorted(allowed))}]")
    cfg = load_cfg()
    client = connect(cfg)
    try:
        if stage in {"prepare", "all"}:
            print("=== prepare backup / nginx / env / docker ===")
            must_run(client, PREPARE_CMD, timeout=180)
        if stage in {"upload", "all"}:
            print("=== upload source ===")
            upload(client)
            print(f"uploaded {len(iter_local_files())} files")
        if stage in {"build", "all"}:
            print("=== build and up ===")
            must_run(client, BUILD_CMD, timeout=1800)
        if stage in {"verify", "all"}:
            print("=== verify ===")
            must_run(client, VERIFY_CMD, timeout=180)
        if stage == "status":
            print("=== status ===")
            must_run(
                client,
                "docker compose -f /opt/aigc-3d-platform/docker-compose.yml --project-directory /opt/aigc-3d-platform ps; echo '===== IMAGES ====='; docker images --format 'table {{.Repository}}\\t{{.Tag}}\\t{{.CreatedSince}}\\t{{.Size}}'; echo '===== LISTEN ====='; ss -lnt | grep -E ':80|:443|:5173|:8080' || true",
                timeout=60,
            )
    finally:
        client.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"DEPLOY_FAILED: {exc}", file=sys.stderr)
        raise

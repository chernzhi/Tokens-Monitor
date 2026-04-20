#!/usr/bin/env python3
"""One-off: SSH to server, upload merge script, docker cp into backend, run merge script.

示例:
  python scripts/remote_merge_run.py --dry-run
  python scripts/remote_merge_run.py --execute
  python scripts/remote_merge_run.py --dry-run --source-employee-id A --target-employee-id B
  python scripts/remote_merge_run.py --execute --source-employee-id A --target-employee-id B
"""
import os
import shlex
import sys
from pathlib import Path

import paramiko

HOST = os.environ.get("SSH_HOST", "192.168.0.135")
USER = os.environ.get("SSH_USER", "root")
PWD = os.environ.get("SSH_PASS", "")
# 传给 merge_duplicate_users_by_name.py 的全部参数（默认同 dry-run）
MERGE_ARGS = sys.argv[1:] if len(sys.argv) > 1 else ["--dry-run"]

_REPO = Path(__file__).resolve().parent.parent
_LOCAL_SCRIPT = _REPO / "backend" / "scripts" / "merge_duplicate_users_by_name.py"
_REMOTE_SCRIPT = "/opt/token-monitor/backend/scripts/merge_duplicate_users_by_name.py"


def main() -> None:
    if not PWD:
        print("Set SSH_PASS", file=sys.stderr)
        sys.exit(1)

    if not _LOCAL_SCRIPT.is_file():
        print(f"Missing {_LOCAL_SCRIPT}", file=sys.stderr)
        sys.exit(1)

    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOST, username=USER, password=PWD, timeout=45)

    def run(cmd: str, timeout: int = 300) -> tuple[str, str, int]:
        _, stdout, stderr = ssh.exec_command(cmd, timeout=timeout)
        out = stdout.read().decode("utf-8", errors="replace")
        err = stderr.read().decode("utf-8", errors="replace")
        code = stdout.channel.recv_exit_status()
        return out, err, code

    run("mkdir -p /opt/token-monitor/backend/scripts", timeout=30)
    sftp = ssh.open_sftp()
    sftp.put(str(_LOCAL_SCRIPT), _REMOTE_SCRIPT)
    sftp.close()
    print(f"Uploaded → {_REMOTE_SCRIPT}")

    quoted = " ".join(shlex.quote(a) for a in MERGE_ARGS)
    # bash: copy host file into running backend container, run python
    inner = (
        "cd /opt/token-monitor && "
        "CID=$(docker compose ps -q backend) && "
        "docker cp /opt/token-monitor/backend/scripts/merge_duplicate_users_by_name.py "
        "${CID}:/app/merge_duplicate_users_by_name.py && "
        f"docker compose exec -T backend python /app/merge_duplicate_users_by_name.py {quoted}"
    )
    print(f"=== merge_duplicate_users_by_name.py {' '.join(MERGE_ARGS)} ===")
    out, err, code = run(inner, timeout=600)
    print(out)
    if err.strip():
        print("stderr:", err)
    print("exit", code)
    ssh.close()
    sys.exit(code)


if __name__ == "__main__":
    main()

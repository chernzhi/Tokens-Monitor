"""Client binary release distribution.

Scans EXTENSION_DIR/client/ for files matching ai-monitor-X.Y.Z.exe,
returns latest semver as JSON, and serves the binary + sha256 sidecar.
"""

import re
from pathlib import Path

from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse

from app.config import settings

router = APIRouter(prefix="/api/release", tags=["release"])

EXTENSION_DIR = Path(getattr(settings, "EXTENSION_DIR", "/opt/token-monitor/extensions"))
CLIENT_DIR = EXTENSION_DIR / "client"

_EXE_RE = re.compile(r"^ai-monitor-(?P<version>\d+\.\d+\.\d+)\.exe$")


def _parse_semver(v: str) -> tuple[int, ...]:
    return tuple(int(x) for x in v.split("."))


def _scan_latest_client() -> dict | None:
    if not CLIENT_DIR.is_dir():
        return None
    best = None
    for f in CLIENT_DIR.iterdir():
        m = _EXE_RE.match(f.name)
        if not m:
            continue
        version = m.group("version")
        sha_file = CLIENT_DIR / (f.name + ".sha256")
        if not sha_file.is_file():
            continue
        if best is None or _parse_semver(version) > _parse_semver(best["version"]):
            sha = sha_file.read_text(encoding="utf-8").strip().split()[0]
            notes_file = CLIENT_DIR / f"ai-monitor-{version}.md"
            notes = notes_file.read_text(encoding="utf-8") if notes_file.is_file() else ""
            best = {
                "version": version,
                "filename": f.name,
                "sha256": sha,
                "size_bytes": f.stat().st_size,
                "release_notes": notes,
            }
    return best


@router.get("/client/latest")
async def latest_client(current: str = "", platform: str = "win32-x64"):
    if platform != "win32-x64":
        raise HTTPException(404, f"platform {platform} not supported")
    info = _scan_latest_client()
    if info is None:
        raise HTTPException(404, "no client release available")
    has_update = False
    if current:
        try:
            has_update = _parse_semver(info["version"]) > _parse_semver(current)
        except ValueError:
            has_update = True
    return {
        "latest_version": info["version"],
        "current_version": current,
        "has_update": has_update,
        "download_url": f"/api/release/client/download/{info['filename']}",
        "sha256": info["sha256"],
        "size_bytes": info["size_bytes"],
        "release_notes": info["release_notes"],
        "mandatory": False,
        "published_at": "",
    }


@router.get("/client/download/{filename}")
async def download_client(filename: str):
    if not _EXE_RE.match(filename):
        raise HTTPException(400, "invalid filename")
    fp = CLIENT_DIR / filename
    if not fp.is_file():
        raise HTTPException(404, "file not found")
    sha_file = CLIENT_DIR / (filename + ".sha256")
    headers = {}
    if sha_file.is_file():
        headers["ETag"] = sha_file.read_text(encoding="utf-8").strip().split()[0]
    return FileResponse(fp, media_type="application/octet-stream", filename=filename, headers=headers)

import os
import tempfile
import unittest
from pathlib import Path

from fastapi.testclient import TestClient


def _make_client(tmp: Path):
    os.environ["EXTENSION_DIR"] = str(tmp)
    # 强制重新读取 settings
    from importlib import reload
    from app import config as cfg
    reload(cfg)
    from app.routers import release
    reload(release)
    from app import main
    reload(main)
    return TestClient(main.app)


class ReleaseClientTests(unittest.TestCase):
    def test_latest_returns_404_when_no_client_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            resp = client.get("/api/release/client/latest?current=3.2.9&platform=win32-x64")
            self.assertEqual(resp.status_code, 404)

    def test_latest_returns_release_info_when_files_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"fake-exe")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text(
                "abcd1234" * 8 + "\n", encoding="utf-8"
            )
            (cdir / "ai-monitor-3.3.0.md").write_text("release notes", encoding="utf-8")
            (cdir / "ai-monitor-3.2.9.exe").write_bytes(b"old")
            (cdir / "ai-monitor-3.2.9.exe.sha256").write_text("00" * 32, encoding="utf-8")

            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/latest?current=3.2.9")
            self.assertEqual(r.status_code, 200)
            body = r.json()
            self.assertEqual(body["latest_version"], "3.3.0")
            self.assertTrue(body["has_update"])
            self.assertEqual(body["size_bytes"], len(b"fake-exe"))
            self.assertIn("3.3.0", body["download_url"])

    def test_latest_no_update_when_current_already_latest(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"x")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text("00" * 32, encoding="utf-8")
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/latest?current=3.3.0")
            self.assertEqual(r.status_code, 200)
            self.assertFalse(r.json()["has_update"])

    def test_download_rejects_path_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/..%2Fpasswd")
            self.assertIn(r.status_code, (400, 404))

    def test_download_404_for_missing_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "client").mkdir()
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/ai-monitor-9.9.9.exe")
            self.assertEqual(r.status_code, 404)

    def test_download_returns_file_with_etag(self):
        with tempfile.TemporaryDirectory() as tmp:
            cdir = Path(tmp) / "client"
            cdir.mkdir()
            (cdir / "ai-monitor-3.3.0.exe").write_bytes(b"payload")
            (cdir / "ai-monitor-3.3.0.exe.sha256").write_text("deadbeef" * 8, encoding="utf-8")
            client = _make_client(Path(tmp))
            r = client.get("/api/release/client/download/ai-monitor-3.3.0.exe")
            self.assertEqual(r.status_code, 200)
            self.assertEqual(r.content, b"payload")
            self.assertEqual(r.headers.get("etag"), "deadbeef" * 8)


if __name__ == "__main__":
    unittest.main()

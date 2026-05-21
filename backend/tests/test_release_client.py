import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient

from app.main import app
from app.routers import release

client = TestClient(app)


class ReleaseClientTests(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp_path = Path(self._tmp.name)
        self.client_dir = self.tmp_path / "client"
        self.client_dir.mkdir()
        self._patches = [
            patch.object(release, "CLIENT_DIR", self.client_dir),
            patch.object(release, "EXTENSION_DIR", self.tmp_path),
        ]
        for p in self._patches:
            p.start()

    def tearDown(self):
        for p in self._patches:
            p.stop()
        self._tmp.cleanup()

    def test_latest_returns_404_when_no_client_files(self):
        resp = client.get("/api/release/client/latest?current=3.2.9&platform=win32-x64")
        self.assertEqual(resp.status_code, 404)

    def test_latest_returns_release_info_when_files_present(self):
        (self.client_dir / "ai-monitor-3.3.0.exe").write_bytes(b"fake-exe")
        (self.client_dir / "ai-monitor-3.3.0.exe.sha256").write_text(
            "abcd1234" * 8 + "\n", encoding="utf-8"
        )
        (self.client_dir / "ai-monitor-3.3.0.md").write_text("release notes", encoding="utf-8")
        (self.client_dir / "ai-monitor-3.2.9.exe").write_bytes(b"old")
        (self.client_dir / "ai-monitor-3.2.9.exe.sha256").write_text("00" * 32, encoding="utf-8")

        r = client.get("/api/release/client/latest?current=3.2.9")
        self.assertEqual(r.status_code, 200)
        body = r.json()
        self.assertEqual(body["latest_version"], "3.3.0")
        self.assertTrue(body["has_update"])
        self.assertEqual(body["size_bytes"], len(b"fake-exe"))
        self.assertIn("3.3.0", body["download_url"])

    def test_latest_no_update_when_current_already_latest(self):
        (self.client_dir / "ai-monitor-3.3.0.exe").write_bytes(b"x")
        (self.client_dir / "ai-monitor-3.3.0.exe.sha256").write_text("00" * 32, encoding="utf-8")
        r = client.get("/api/release/client/latest?current=3.3.0")
        self.assertEqual(r.status_code, 200)
        self.assertFalse(r.json()["has_update"])

    def test_download_rejects_bad_filename(self):
        r = client.get("/api/release/client/download/ai-monitor-1.2.3.exe.bak")
        self.assertEqual(r.status_code, 400)

    def test_download_404_for_missing_file(self):
        r = client.get("/api/release/client/download/ai-monitor-9.9.9.exe")
        self.assertEqual(r.status_code, 404)

    def test_download_returns_file_with_etag(self):
        (self.client_dir / "ai-monitor-3.3.0.exe").write_bytes(b"payload")
        (self.client_dir / "ai-monitor-3.3.0.exe.sha256").write_text(
            "deadbeef" * 8, encoding="utf-8"
        )
        r = client.get("/api/release/client/download/ai-monitor-3.3.0.exe")
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.content, b"payload")
        self.assertEqual(r.headers.get("etag"), "deadbeef" * 8)


if __name__ == "__main__":
    unittest.main()

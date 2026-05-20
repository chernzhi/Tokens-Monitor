from __future__ import annotations

import unittest

from datetime import datetime, timezone
from types import SimpleNamespace

from app.routers.collect import UsageRecordIn, _cursor_log_matches_local_estimate, _cursor_log_matches_official, _cursor_reporting_fields, _cursor_strict_merge_candidate
from app.routers.dashboard import _is_cursor_excluded_from_main_stats


def _record(
    *,
    source: str,
    vendor: str = "cursor",
    source_kind: str | None = None,
    accuracy: str | None = None,
    correlation_key: str | None = None,
    endpoint: str | None = None,
    source_app: str | None = "cursor",
) -> UsageRecordIn:
    return UsageRecordIn(
        client_id="client-1",
        user_name="Cursor User",
        user_id="10001",
        source=source,
        model="claude-4-sonnet",
        vendor=vendor,
        prompt_tokens=700,
        completion_tokens=300,
        total_tokens=1000,
        request_time="2026-05-19T12:00:00Z",
        request_id=f"{source}-1",
        source_app=source_app,
        endpoint=endpoint,
        source_kind=source_kind,
        accuracy=accuracy,
        correlation_key=correlation_key,
    )


class CursorReportingMergeTests(unittest.TestCase):
    def test_cursor_official_record_defaults_to_exact_official_unmatched(self):
        fields = _cursor_reporting_fields(
            _record(source="cursor-official-api", source_kind=None, accuracy=None),
            provider_key="cursor",
        )

        self.assertEqual(fields.source_kind, "official")
        self.assertEqual(fields.accuracy, "exact")
        self.assertEqual(fields.merge_status, "unmatched")

    def test_cursor_local_estimate_defaults_to_estimated_unmatched(self):
        fields = _cursor_reporting_fields(
            _record(source="client-mitm-estimate", source_kind=None, accuracy=None),
            provider_key="cursor",
        )

        self.assertEqual(fields.source_kind, "local_estimate")
        self.assertEqual(fields.accuracy, "estimated")
        self.assertEqual(fields.merge_status, "unmatched")

    def test_non_cursor_records_do_not_get_cursor_merge_defaults(self):
        fields = _cursor_reporting_fields(
            _record(source="client", vendor="anthropic", source_kind=None, accuracy=None),
            provider_key="anthropic",
        )

        self.assertIsNone(fields.source_kind)
        self.assertIsNone(fields.accuracy)
        self.assertIsNone(fields.merge_status)

    def test_strict_merge_requires_same_correlation_key_and_single_local_context(self):
        official = _record(
            source="cursor-official-api",
            source_kind="official",
            accuracy="exact",
            correlation_key="cursor-event-1",
        )
        local = _record(
            source="client-mitm-estimate",
            source_kind="local_estimate",
            accuracy="estimated",
            correlation_key="cursor-event-1",
            endpoint="/v1/chat/completions",
            source_app="cursor",
        )

        self.assertTrue(_cursor_strict_merge_candidate(official, local, provider_key="cursor"))

    def test_strict_merge_rejects_different_correlation_key(self):
        official = _record(
            source="cursor-official-api",
            source_kind="official",
            accuracy="exact",
            correlation_key="cursor-event-1",
        )
        different = _record(
            source="client-mitm-estimate",
            source_kind="local_estimate",
            accuracy="estimated",
            correlation_key="cursor-event-2",
            endpoint="/v1/chat/completions",
        )

        self.assertFalse(_cursor_strict_merge_candidate(official, different, provider_key="cursor"))

    def test_cursor_log_classifiers_identify_actual_merge_candidates_without_correlation_key(self):
        request_at = datetime(2026, 5, 19, 12, 0, tzinfo=timezone.utc)
        official = SimpleNamespace(
            provider="cursor",
            source_kind="official",
            accuracy="exact",
            merge_status="unmatched",
            correlation_key=None,
            user_id=1,
            model_name="claude-4-sonnet",
            request_at=request_at,
        )
        local = SimpleNamespace(
            provider="cursor",
            source_kind="local_estimate",
            accuracy="estimated",
            merge_status="unmatched",
            correlation_key=None,
            user_id=1,
            model_name="claude-4-sonnet",
            request_at=request_at,
        )

        self.assertTrue(_cursor_log_matches_official(official))
        self.assertTrue(_cursor_log_matches_local_estimate(local))

    def test_dashboard_excludes_unmatched_or_suppressed_cursor_local_estimates(self):
        self.assertTrue(_is_cursor_excluded_from_main_stats("cursor", "client-mitm-estimate", None, None))
        self.assertTrue(_is_cursor_excluded_from_main_stats("cursor", "client", "local_estimate", "unmatched"))
        self.assertTrue(_is_cursor_excluded_from_main_stats("cursor", "client", "local_estimate", "suppressed"))
        self.assertFalse(_is_cursor_excluded_from_main_stats("cursor", "cursor-official-api", "official", "unmatched"))
        self.assertFalse(_is_cursor_excluded_from_main_stats("anthropic", "client-mitm-estimate", None, None))


if __name__ == "__main__":
    unittest.main()

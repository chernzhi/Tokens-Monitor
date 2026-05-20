package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCursorLocalEstimateRecordSerializesMergeFields(t *testing.T) {
	rec := UsageRecord{
		Vendor:       "cursor",
		Model:        "cursor-opaque",
		Endpoint:     "/v1/chat/completions",
		PromptTokens: 10,
		TotalTokens:  30,
		Source:       opaqueSourceEstimate,
		SourceApp:    "cursor",
		SourceKind:   "local_estimate",
		Accuracy:     "estimated",
		MergeStatus:  "unmatched",
	}

	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{
		`"source_kind":"local_estimate"`,
		`"accuracy":"estimated"`,
		`"merge_status":"unmatched"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("serialized record missing %s in %s", want, body)
		}
	}
}

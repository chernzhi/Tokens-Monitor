package main

import "testing"

func TestCopilotResponsesStreamEstimatorRecordsDeltaEstimate(t *testing.T) {
	reporter := NewReporter(&Config{ServerURL: "http://127.0.0.1:1", UserName: "tester", UserID: "tester"})
	estimator := newCopilotResponsesStreamEstimator("github-copilot", "/responses", "gpt-4.1", "vscode", 1.5, reporter)
	estimator.Observe([]byte(`{"delta":"hello "}`))
	estimator.Observe([]byte(`{"delta":"world"}`))
	estimator.Flush()

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(reporter.queue))
	}
	record := reporter.queue[0]
	if record.PromptTokens != 0 || record.CompletionTokens != 2 || record.TotalTokens != 2 {
		t.Fatalf("unexpected token estimate: %+v", record)
	}
	if record.Model != "gpt-4.1·opaque(估算)" || record.SourceApp != "vscode" || record.CostMultiplier != 1.5 {
		t.Fatalf("unexpected metadata: %+v", record)
	}
}

func TestCopilotResponsesStreamEstimatorSkipsWhenExactUsageArrives(t *testing.T) {
	reporter := NewReporter(&Config{ServerURL: "http://127.0.0.1:1", UserName: "tester", UserID: "tester"})
	estimator := newCopilotResponsesStreamEstimator("github-copilot", "/responses", "gpt-4.1", "vscode", 0, reporter)
	estimator.Observe([]byte(`{"delta":"hello"}`))
	estimator.Observe([]byte(`{"type":"response.completed","response":{"model":"gpt-4.1","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}}`))
	estimator.Flush()

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.queue) != 0 {
		t.Fatalf("estimator should not duplicate exact usage, queue len = %d", len(reporter.queue))
	}
}

func TestCopilotResponsesStreamEstimatorDoesNotDoubleCountSnapshotAfterDelta(t *testing.T) {
	reporter := NewReporter(&Config{ServerURL: "http://127.0.0.1:1", UserName: "tester", UserID: "tester"})
	estimator := newCopilotResponsesStreamEstimator("github-copilot", "/responses", "", "vscode", 0, reporter)
	estimator.Observe([]byte(`{"delta":"hello"}`))
	estimator.Observe([]byte(`{"item":{"content":[{"text":"hello world"}]}}`))
	estimator.Flush()

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(reporter.queue))
	}
	if reporter.queue[0].CompletionTokens != 1 {
		t.Fatalf("completion tokens = %d, want 1", reporter.queue[0].CompletionTokens)
	}
}

func TestCopilotResponsesStreamEstimatorResetsBetweenResponses(t *testing.T) {
	reporter := NewReporter(&Config{ServerURL: "http://127.0.0.1:1", UserName: "tester", UserID: "tester"})
	estimator := newCopilotResponsesStreamEstimator("github-copilot", "/responses", "gpt-4.1", "vscode", 0, reporter)
	estimator.Observe([]byte(`{"type":"response.completed","response":{"model":"gpt-4.1","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}}`))
	estimator.Observe([]byte(`{"delta":"second response"}`))
	estimator.Flush()

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(reporter.queue))
	}
	if reporter.queue[0].CompletionTokens != 3 {
		t.Fatalf("completion tokens = %d, want 3", reporter.queue[0].CompletionTokens)
	}
}

func TestCopilotResponsesStreamEventRecognizesNonDeltaChunks(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`{"item":{"content":[{"text":"hello"}]}}`),
		[]byte(`{"item_id":"abc","content_index":0}`),
		[]byte(`{"response":{"id":"r1","usage":null}}`),
		[]byte(`{"arguments":"{}","call_id":"call_1"}`),
	} {
		if !isCopilotResponsesStreamEvent("github-copilot", "/responses", payload) {
			t.Fatalf("payload should be recognized as responses stream event: %s", payload)
		}
	}
}

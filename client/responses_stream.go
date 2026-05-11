package main

import (
	"encoding/json"
	"strings"
)

type copilotResponsesStreamEstimator struct {
	vendor         string
	endpoint       string
	modelHint      string
	sourceApp      string
	costMultiplier float64
	reporter       *Reporter
	textBytes      int
	sawDelta       bool
	sawExactUsage  bool
}

func newCopilotResponsesStreamEstimator(vendor, endpoint, modelHint, sourceApp string, costMultiplier float64, reporter *Reporter) *copilotResponsesStreamEstimator {
	if reporter == nil || !isCopilotResponsesEndpoint(vendor, endpoint) {
		return nil
	}
	return &copilotResponsesStreamEstimator{
		vendor:         vendor,
		endpoint:       endpoint,
		modelHint:      modelHint,
		sourceApp:      sourceApp,
		costMultiplier: costMultiplier,
		reporter:       reporter,
	}
}

func isCopilotResponsesEndpoint(vendor, endpoint string) bool {
	return strings.EqualFold(strings.TrimSpace(vendor), "github-copilot") &&
		strings.Contains(strings.ToLower(endpoint), "/responses")
}

func isCopilotResponsesStreamDelta(vendor, endpoint string, payload []byte) bool {
	if !isCopilotResponsesEndpoint(vendor, endpoint) {
		return false
	}
	textBytes, hasDelta := copilotResponsesDeltaTextBytes(payload)
	return hasDelta && textBytes > 0
}

func isCopilotResponsesStreamEvent(vendor, endpoint string, payload []byte) bool {
	if !isCopilotResponsesEndpoint(vendor, endpoint) {
		return false
	}
	var root map[string]interface{}
	if json.Unmarshal(payload, &root) != nil {
		return false
	}
	for _, key := range []string{"delta", "item", "item_id", "content_index", "response", "arguments", "call_id"} {
		if _, ok := root[key]; ok {
			return true
		}
	}
	if eventType, _ := root["type"].(string); strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "response.") {
		return true
	}
	return false
}

func (e *copilotResponsesStreamEstimator) Observe(payload []byte) {
	if e == nil {
		return
	}
	terminal := copilotResponsesStreamTerminal(payload)
	if usage := ExtractUsage(e.vendor, payload); usage != nil && usage.TotalTokens > 0 {
		e.sawExactUsage = true
		e.textBytes = 0
		if terminal {
			e.resetCycle()
		}
		return
	}
	if e.modelHint == "" {
		e.modelHint = inferModelHint(payload)
	}

	if textBytes, hasDelta := copilotResponsesDeltaTextBytes(payload); hasDelta {
		e.sawDelta = true
		e.textBytes += textBytes
	} else if !e.sawDelta {
		e.textBytes += copilotResponsesSnapshotTextBytes(payload)
	}

	if terminal {
		e.Flush()
	}
}

func (e *copilotResponsesStreamEstimator) Flush() {
	if e == nil {
		return
	}
	if e.sawExactUsage || e.textBytes <= 0 {
		e.resetCycle()
		return
	}
	completionTokens := e.textBytes / 4
	if completionTokens < 1 {
		completionTokens = 1
	}
	model := e.modelHint
	if model == "" {
		model = "github-copilot-responses"
	}
	e.reporter.Add(UsageRecord{
		Vendor:           e.vendor,
		Model:            opaqueModelLabelWithHint(e.vendor, model),
		Endpoint:         e.endpoint,
		PromptTokens:     0,
		CompletionTokens: completionTokens,
		TotalTokens:      completionTokens,
		CostMultiplier:   e.costMultiplier,
		Source:           opaqueSourceEstimate,
		SourceApp:        e.sourceApp,
	})
	e.resetCycle()
}

func (e *copilotResponsesStreamEstimator) resetCycle() {
	e.textBytes = 0
	e.sawDelta = false
	e.sawExactUsage = false
}

func copilotResponsesDeltaTextBytes(payload []byte) (int, bool) {
	var root map[string]interface{}
	if json.Unmarshal(payload, &root) != nil {
		return 0, false
	}
	delta, ok := root["delta"].(string)
	if !ok || delta == "" {
		return 0, false
	}
	return len(delta), true
}

func copilotResponsesSnapshotTextBytes(payload []byte) int {
	var root map[string]interface{}
	if json.Unmarshal(payload, &root) != nil {
		return 0
	}
	item, ok := root["item"].(map[string]interface{})
	if !ok {
		return 0
	}
	content, ok := item["content"].([]interface{})
	if !ok {
		return 0
	}
	total := 0
	for _, part := range content {
		partMap, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := partMap["text"].(string); ok {
			total += len(text)
		}
	}
	return total
}

func copilotResponsesStreamTerminal(payload []byte) bool {
	var root map[string]interface{}
	if json.Unmarshal(payload, &root) != nil {
		return false
	}
	if done, ok := root["done"].(bool); ok && done {
		return true
	}
	eventType, _ := root["type"].(string)
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	return strings.HasPrefix(eventType, "response.completed") ||
		strings.HasPrefix(eventType, "response.done") ||
		strings.HasPrefix(eventType, "response.failed") ||
		strings.HasPrefix(eventType, "response.incomplete") ||
		strings.HasPrefix(eventType, "response.cancelled")
}

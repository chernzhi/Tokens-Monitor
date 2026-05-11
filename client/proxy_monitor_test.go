package main

import (
	"errors"
	"io"
	"testing"
)

func TestShouldMonitorAIEndpointSkipsTelemetry(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{endpoint: "/ces/v1/telemetry/intake", want: false},
		{endpoint: "/v1/telemetry/intake", want: false},
		{endpoint: "/v1/models", want: false},
		{endpoint: "/api/v1/models", want: false},
		{endpoint: "/models/session", want: false},
		{endpoint: "/agents", want: false},
		{endpoint: "/agents/swe/models", want: false},
		{endpoint: "/extensions-control", want: false},
		{endpoint: "/tev1/v1/rgstr", want: false},
		{endpoint: "/backend-api/accounts/check", want: false},
		{endpoint: "/backend-api/codex/responses", want: true},
		{endpoint: "/v1/chat/completions", want: true},
	}

	for _, tc := range cases {
		if got := shouldMonitorAIEndpoint(tc.endpoint); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.endpoint, got, tc.want)
		}
	}
}

func TestExpectedDisconnectErrors(t *testing.T) {
	cases := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		errors.New("read tcp 127.0.0.1:18090->127.0.0.1:57099: wsarecv: An existing connection was forcibly closed by the remote host."),
		errors.New("write tcp 127.0.0.1:18090->127.0.0.1:57099: broken pipe"),
		errors.New("read tcp: connection reset by peer"),
	}

	for _, err := range cases {
		if !isExpectedDisconnectError(err) {
			t.Fatalf("expected disconnect not recognized: %v", err)
		}
	}
}

func TestLinkLocalMetadataHost(t *testing.T) {
	if !isLinkLocalMetadataHost("169.254.169.254") {
		t.Fatal("metadata host should be link-local metadata")
	}
	if isLinkLocalMetadataHost("api.openai.com") {
		t.Fatal("api.openai.com should not be metadata host")
	}
}

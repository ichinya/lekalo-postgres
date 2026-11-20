package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeRejectsInvalidRequests(t *testing.T) {
	validID := "sha256:" + strings.Repeat("a", 64)
	envelope := `{"protocol":"lekalo/runtime/v1","request_id":"` + validID + `","operation":"describe"}`
	cases := map[string]string{
		"invalid UTF-8":     "\xff",
		"wrong protocol":    strings.Replace(envelope, protocol, "lekalo/runtime/v2", 1),
		"wrong request ID":  strings.Replace(envelope, validID, "sha256:123", 1),
		"missing operation": strings.Replace(envelope, `,"operation":"describe"`, "", 1),
		"extra document":    envelope + "{}",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := serve(strings.NewReader(input), &output); err == nil || err.Error() != "request-envelope" {
				t.Fatalf("expected envelope refusal, got %v", err)
			}
			if output.Len() != 0 {
				t.Fatalf("refused request produced stdout: %s", &output)
			}
		})
	}
}

func TestServeBoundsInput(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader(strings.Repeat(" ", maxRequestSize+1))
	if err := serve(input, &output); err == nil || err.Error() != "request-size" {
		t.Fatalf("expected size refusal, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("oversized request produced stdout: %s", &output)
	}
}

func TestServeRefusesSourceChecks(t *testing.T) {
	input := `{"protocol":"lekalo/runtime/v1","request_id":"sha256:` +
		strings.Repeat("a", 64) + `","operation":"check","input":{"rules":[{}]}}`
	var output bytes.Buffer
	if err := serve(strings.NewReader(input), &output); err == nil || err.Error() != "contract-checks-unsupported" {
		t.Fatalf("expected rules refusal, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("refused check produced stdout: %s", &output)
	}
}

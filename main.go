// lekalo-postgres exchanges one request and one response over stdin/stdout.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"unicode/utf8"
)

const (
	protocol       = "lekalo/runtime/v1"
	rulesAPI       = "lekalo/contract-rules/v1"
	maxRequestSize = 8 * 1024 * 1024
)

var requestIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type response struct {
	Protocol  string `json:"protocol"`
	RequestID string `json:"request_id"`
	Result    any    `json:"result"`
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(input io.Reader, output io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(input, maxRequestSize+1))
	if err != nil {
		return errors.New("request-read")
	}
	if len(raw) > maxRequestSize {
		return errors.New("request-size")
	}

	var fields map[string]json.RawMessage
	var wire, id string
	var operation *string
	if !utf8.Valid(raw) || json.Unmarshal(raw, &fields) != nil ||
		json.Unmarshal(fields["protocol"], &wire) != nil || wire != protocol ||
		json.Unmarshal(fields["request_id"], &id) != nil || !requestIDPattern.MatchString(id) ||
		json.Unmarshal(fields["operation"], &operation) != nil || operation == nil {
		return errors.New("request-envelope")
	}

	result, err := handle(*operation, fields["input"])
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response{Protocol: wire, RequestID: id, Result: result})
}

func handle(operation string, input json.RawMessage) (any, error) {
	switch operation {
	case "describe":
		return map[string]any{
			"id":           "lekalo-postgres",
			"version":      "0.1.0",
			"rules_api":    []string{rulesAPI},
			"capabilities": []string{"storage.postgres"},
			"operations": []string{
				"check", "storage.matrix", "storage.type", "storage.ddl", "storage.table",
				"storage.join", "storage.default", "storage.sequence", "storage.index-name",
			},
		}, nil
	case "check":
		var check struct {
			Rules []json.RawMessage `json:"rules"`
		}
		if len(input) != 0 && (json.Unmarshal(input, &check) != nil || len(check.Rules) != 0) {
			return nil, errors.New("contract-checks-unsupported")
		}
		return map[string]any{"state": "complete", "facts": map[string]any{}}, nil
	default:
		value, refusal := dispatch(operation, input)
		if refusal != nil {
			return map[string]any{"error": refusal}, nil
		}
		return map[string]any{"value": value}, nil
	}
}

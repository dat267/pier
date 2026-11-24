package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func parseSystemOneResponse(data []byte, transport systemOneTransport) (*orderedJSONFields, error) {
	unexpected := func() error { return fmt.Errorf("%s returned an unexpected response", transport.label) }
	response, err := decodeOrderedJSONFields(data)
	if err != nil {
		return nil, unexpected()
	}
	if !transport.cloudflareEnvelope {
		return response, nil
	}
	if successRaw, ok := response.raw("success"); ok {
		var success bool
		if json.Unmarshal(successRaw, &success) == nil && !success {
			errorsRaw, _ := response.raw("errors")
			return nil, errors.New(cloudflareSystemOneError(errorsRaw))
		}
	}
	resultRaw, ok := response.raw("result")
	if !ok {
		return nil, unexpected()
	}
	result, err := decodeOrderedJSONFields(resultRaw)
	if err != nil {
		return nil, unexpected()
	}
	if _, ok := result.raw("answers"); ok {
		return result, nil
	}
	stateRaw, hasState := result.raw("state")
	// The reference interpolates `${String(result.state)}`, so a non-string
	// state follows JavaScript's rendering: an object is "[object Object]", an
	// array joins with commas. Embedding the raw JSON member would print Go's
	// view of it instead.
	state := "undefined"
	if hasState {
		state = rawJavaScriptStateText(stateRaw)
	}
	if state != "Completed" {
		return nil, fmt.Errorf("%s run did not complete (state: %s)", transport.label, state)
	}
	nestedRaw, ok := result.raw("result")
	if !ok {
		return nil, unexpected()
	}
	nested, err := decodeOrderedJSONFields(nestedRaw)
	if err != nil {
		return nil, unexpected()
	}
	return nested, nil
}

func cloudflareSystemOneError(raw json.RawMessage) string {
	var entries []map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) == nil {
		messages := make([]string, 0, len(entries))
		for _, entry := range entries {
			if message, ok := classifierString(entry["message"]); ok {
				messages = append(messages, message)
			}
		}
		if len(messages) > 0 {
			return cloudflareLabel + " error: " + strings.Join(messages, "; ")
		}
	}
	return cloudflareLabel + " request failed"
}

// rawJavaScriptStateText renders a state member the way `${String(value)}` does,
// decoding it first so an object reports "[object Object]" and an array joins
// with commas. A member that is not valid JSON keeps its raw text, which is what
// the previous rendering did and the only thing left to report.
func rawJavaScriptStateText(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return string(raw)
	}
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	return JSValueToString(value)
}

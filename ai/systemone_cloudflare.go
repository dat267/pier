package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseSystemOneResponse(data []byte, transport systemOneTransport) (*orderedJSONFields, error) {
	response, err := decodeOrderedJSONFields(data)
	if err != nil {
		return nil, fmt.Errorf("%s returned an unexpected response", transport.label)
	}
	if !transport.cloudflareEnvelope {
		return response, nil
	}
	if successRaw, ok := response.raw("success"); ok {
		var success bool
		if json.Unmarshal(successRaw, &success) == nil && !success {
			errorsRaw, _ := response.raw("errors")
			return nil, fmt.Errorf("%s", cloudflareSystemOneError(errorsRaw))
		}
	}
	resultRaw, ok := response.raw("result")
	if !ok {
		return nil, fmt.Errorf("%s returned an unexpected response", transport.label)
	}
	result, err := decodeOrderedJSONFields(resultRaw)
	if err != nil {
		return nil, fmt.Errorf("%s returned an unexpected response", transport.label)
	}
	if _, ok := result.raw("answers"); ok {
		return result, nil
	}
	stateRaw, hasState := result.raw("state")
	state := "undefined"
	if hasState {
		if decoded, ok := classifierString(stateRaw); ok {
			state = decoded
		} else {
			state = string(stateRaw)
		}
	}
	if state != "Completed" {
		return nil, fmt.Errorf("%s run did not complete (state: %s)", transport.label, state)
	}
	nestedRaw, ok := result.raw("result")
	if !ok {
		return nil, fmt.Errorf("%s returned an unexpected response", transport.label)
	}
	nested, err := decodeOrderedJSONFields(nestedRaw)
	if err != nil {
		return nil, fmt.Errorf("%s returned an unexpected response", transport.label)
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

package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// JsonRpcId is upstream's `string | number` request id. The zero value is
// the number 0; String reports whether the id was a string.
type JsonRpcId struct {
	Str   string
	Num   float64
	IsStr bool
}

// StringId builds a string id.
func StringId(s string) JsonRpcId { return JsonRpcId{Str: s, IsStr: true} }

// NumberId builds a numeric id.
func NumberId(n float64) JsonRpcId { return JsonRpcId{Num: n} }

// String renders the id as JSON text (a quoted string or a bare number).
func (id JsonRpcId) String() string {
	if id.IsStr {
		enc, _ := json.Marshal(id.Str)
		return string(enc)
	}
	return formatJSONNumber(id.Num)
}

// MarshalJSON encodes the id as a JSON string or number.
func (id JsonRpcId) MarshalJSON() ([]byte, error) {
	if id.IsStr {
		return json.Marshal(id.Str)
	}
	return []byte(formatJSONNumber(id.Num)), nil
}

// UnmarshalJSON decodes either arm; other JSON types are rejected (upstream
// isJsonRpcId).
func (id *JsonRpcId) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		id.IsStr, id.Num = true, 0
		return json.Unmarshal(data, &id.Str)
	}
	var num float64
	if err := json.Unmarshal(data, &num); err != nil {
		return fmt.Errorf("json-rpc id must be a string or a finite number")
	}
	id.IsStr, id.Str, id.Num = false, "", num
	return nil
}

func formatJSONNumber(n float64) string {
	enc, _ := json.Marshal(n)
	return string(enc)
}

// JsonRpcRequest is a request expecting a response (upstream
// JsonRpcRequest).
type JsonRpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JsonRpcId       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JsonRpcNotification is a one-way message (upstream JsonRpcNotification).
type JsonRpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JsonRpcErrorObject is the `error` member of a failure response (upstream
// JsonRpcErrorObject).
type JsonRpcErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JsonRpcResponse is upstream's success-or-error union, kept as a struct so
// decoders can branch on the members without a second parse.
type JsonRpcResponse struct {
	JSONRPC string              `json:"jsonrpc"`
	ID      JsonRpcId           `json:"id"`
	Result  json.RawMessage     `json:"result,omitempty"`
	Error   *JsonRpcErrorObject `json:"error,omitempty"`
}

// IsJsonRpcId mirrors upstream's isJsonRpcId on a decoded value. Numbers
// arrive as json.Number when decoded with UseNumber.
func IsJsonRpcId(value any) bool {
	switch v := value.(type) {
	case string:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case json.Number:
		n, err := v.Float64()
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	return false
}

// IsJsonRpcRequest mirrors upstream's isJsonRpcRequest on a decoded value.
func IsJsonRpcRequest(message any) bool {
	m, ok := message.(map[string]any)
	if !ok {
		return false
	}
	if m["jsonrpc"] != JSONRPCVersion || !IsJsonRpcId(m["id"]) {
		return false
	}
	_, ok = m["method"].(string)
	return ok
}

// IsJsonRpcNotification mirrors upstream's isJsonRpcNotification.
func IsJsonRpcNotification(message any) bool {
	m, ok := message.(map[string]any)
	if !ok {
		return false
	}
	if m["jsonrpc"] != JSONRPCVersion {
		return false
	}
	if _, has := m["id"]; has {
		return false
	}
	_, ok = m["method"].(string)
	return ok
}

// IsJsonRpcResponse mirrors upstream's isJsonRpcResponse.
func IsJsonRpcResponse(message any) bool {
	m, ok := message.(map[string]any)
	if !ok || m["jsonrpc"] != JSONRPCVersion || !IsJsonRpcId(m["id"]) {
		return false
	}
	if _, has := m["result"]; has {
		_, hasErr := m["error"]
		return !hasErr
	}
	errObj, hasErr := m["error"].(map[string]any)
	if !hasErr || errObj == nil {
		return false
	}
	switch errObj["code"].(type) {
	case float64, json.Number:
	default:
		return false
	}
	_, ok = errObj["message"].(string)
	return ok
}

// ParseJsonRpcMessage validates a decoded value as exactly one JSON-RPC
// message (upstream parseJsonRpcMessage); anything else is an
// InvalidRequest McpError.
func ParseJsonRpcMessage(value any) error {
	if IsJsonRpcRequest(value) || IsJsonRpcNotification(value) || IsJsonRpcResponse(value) {
		return nil
	}
	return &McpError{Code: JSONRPCErrorCode.InvalidRequest, Msg: "Invalid JSON-RPC message"}
}

// ParseMessageBytes decodes raw JSON bytes and validates them as one
// JSON-RPC message.
func ParseMessageBytes(data []byte) error {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return &McpError{Code: JSONRPCErrorCode.ParseError, Msg: "Invalid JSON"}
	}
	return ParseJsonRpcMessage(value)
}

// DecodeMessage decodes raw JSON bytes into the concrete message shape:
// *JsonRpcRequest, *JsonRpcNotification, or *JsonRpcResponse.
func DecodeMessage(data []byte) (any, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, &McpError{Code: JSONRPCErrorCode.ParseError, Msg: "Invalid JSON"}
	}
	if err := ParseJsonRpcMessage(value); err != nil {
		return nil, err
	}
	if IsJsonRpcRequest(value) {
		var request JsonRpcRequest
		if err := jsonUnmarshalOrdered(data, &request); err != nil {
			return nil, err
		}
		return &request, nil
	}
	if IsJsonRpcNotification(value) {
		var notification JsonRpcNotification
		if err := jsonUnmarshalOrdered(data, &notification); err != nil {
			return nil, err
		}
		return &notification, nil
	}
	var response JsonRpcResponse
	if err := jsonUnmarshalOrdered(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// jsonUnmarshalOrdered decodes a message while preserving the raw params and
// result bytes (json.RawMessage handles that natively).
func jsonUnmarshalOrdered(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

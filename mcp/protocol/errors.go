package protocol

import "fmt"

// JSONRPCVersion is the required jsonrpc member value.
const JSONRPCVersion = "2.0"

// JSONRPCErrorCode mirrors JSON_RPC_ERROR_CODES.
var JSONRPCErrorCode = struct {
	ParseError     int
	InvalidRequest int
	MethodNotFound int
	InvalidParams  int
	InternalError  int
}{
	ParseError:     -32700,
	InvalidRequest: -32600,
	MethodNotFound: -32601,
	InvalidParams:  -32602,
	InternalError:  -32603,
}

// McpError is an MCP-level error carrying a JSON-RPC code and optional data
// (upstream McpError).
type McpError struct {
	Code int
	Data any
	Msg  string
}

func (e *McpError) Error() string { return e.Msg }

// McpConnectionClosedError signals a closed transport (upstream
// McpConnectionClosedError, default message "MCP connection closed").
type McpConnectionClosedError struct{ Msg string }

func (e *McpConnectionClosedError) Error() string {
	if e.Msg == "" {
		return "MCP connection closed"
	}
	return e.Msg
}

// McpTimeoutError signals a request timeout (upstream McpTimeoutError).
type McpTimeoutError struct{ TimeoutMs int64 }

func (e *McpTimeoutError) Error() string {
	return fmt.Sprintf("MCP request timed out after %dms", e.TimeoutMs)
}

// McpAbortError signals an aborted request (upstream McpAbortError, whose JS
// name is "AbortError").
type McpAbortError struct{ Msg string }

func (e *McpAbortError) Error() string {
	if e.Msg == "" {
		return "MCP request aborted"
	}
	return e.Msg
}

// ToError mirrors upstream's toError: pass the value through when it is
// already an error, wrap anything else.
func ToError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return fmt.Errorf("%v", value)
}

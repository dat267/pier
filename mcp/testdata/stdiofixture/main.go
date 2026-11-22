// stdiofixture is the Go port of packages/mcp/test/fixtures/stdio-server.mjs:
// a newline-delimited MCP server that answers initialize, tools/list,
// tools/call, and ping, errors on other methods, and logs to stderr.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dat267/pier/ai"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type outbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	fmt.Fprintln(os.Stderr, "stdio fixture ready")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err != nil {
				return
			}
			continue
		}
		var message message
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			// The reference's JSON.parse throws, which ends its readline loop.
			return
		}
		if message.ID == nil {
			if message.Method == "notifications/initialized" && os.Getenv("PIER_MCP_CANCEL_REQUEST_FIXTURE") == "1" {
				fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","id":"server-request","method":"wait"}`)
				fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"server-request"}}`)
			}
			// Notifications do not need a response.
			continue
		}
		var result json.RawMessage
		switch message.Method {
		case "initialize":
			result = json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"stdio-fixture","version":"1.0.0"}}`)
		case "tools/list":
			result = json.RawMessage(`{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}`)
		case "tools/call":
			var params struct {
				Arguments map[string]json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(message.Params, &params)
			// The reference writes `String(message.params.arguments.text)`: a
			// number renders as its digits, a present null as "null", and an
			// absent field as "undefined". The raw member distinguishes the last
			// two, which an unmarshalled nil cannot.
			text := "undefined"
			if raw, ok := params.Arguments["text"]; ok {
				var value any
				_ = json.Unmarshal(raw, &value)
				if value == nil {
					text = "null"
				} else {
					text = ai.JSValueToString(value)
				}
			}
			result = json.RawMessage(`{"content":[{"type":"text","text":` + mustJSON(text) + `}]}`)
		case "ping":
			result = json.RawMessage(`{}`)
		default:
			writeMessage(outbound{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32601, Message: "not found"}}, message.ID)
			continue
		}
		writeMessage(outbound{JSONRPC: "2.0", ID: message.ID, Result: result}, message.ID)
	}
}

// writeMessage emits one JSON-RPC line exactly as the reference's
// `JSON.stringify({ jsonrpc, id, result })` (or the error form) does: no HTML
// escaping, and the id spliced back as the caller sent it, since
// JSON.stringify would renumber a large integer id.
func writeMessage(message outbound, id json.RawMessage) {
	var buffer bytes.Buffer
	buffer.WriteString(`{"jsonrpc":`)
	buffer.WriteString(mustJSON(message.JSONRPC))
	buffer.WriteString(`,"id":`)
	buffer.Write(id)
	if message.Error != nil {
		buffer.WriteString(`,"error":`)
		buffer.WriteString(mustJSON(rpcError{Code: message.Error.Code, Message: message.Error.Message}))
	} else {
		buffer.WriteString(`,"result":`)
		buffer.Write(message.Result)
	}
	buffer.WriteString("}\n")
	_, _ = os.Stdout.Write(buffer.Bytes())
}

// mustJSON encodes value the way JSON.stringify does, so `<`, `>` and `&` stay
// literal rather than becoming \u003c, \u003e and \u0026 as encoding/json does.
func mustJSON(value any) string {
	enc, err := ai.MarshalJSON(value)
	if err != nil {
		panic(err)
	}
	return string(enc)
}

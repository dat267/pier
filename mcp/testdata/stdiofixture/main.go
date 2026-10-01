// stdiofixture is the Go port of packages/mcp/test/fixtures/stdio-server.mjs:
// a newline-delimited MCP server that answers initialize, tools/list,
// tools/call, and ping, errors on other methods, and logs to stderr.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	encoder := json.NewEncoder(os.Stdout)
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
			if err != nil {
				return
			}
			continue
		}
		if message.ID == nil {
			// A notification; the fixture has no use for one.
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
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(message.Params, &params)
			result = json.RawMessage(`{"content":[{"type":"text","text":` + mustJSON(params.Arguments.Text) + `}]}`)
		case "ping":
			result = json.RawMessage(`{}`)
		default:
			_ = encoder.Encode(outbound{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32601, Message: "not found"}})
			continue
		}
		_ = encoder.Encode(outbound{JSONRPC: "2.0", ID: message.ID, Result: result})
	}
}

func mustJSON(value string) string {
	enc, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(enc)
}

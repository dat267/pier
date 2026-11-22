// stubbornfixture is the Go port of
// packages/mcp/test/fixtures/stubborn-server.mjs: answers initialize, spawns
// a grandchild that outlives stdin, and ignores stdin EOF and SIGTERM.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "grandchild" {
		// Ignores SIGTERM and outlives stdin; the test asserts the transport
		// kills the whole process group.
		signal.Ignore(syscall.SIGTERM)
		select {}
	}
	signal.Ignore(syscall.SIGTERM)
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	grandchild := exec.Command(self, "grandchild")
	if err := grandchild.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "grandchild %d\n", grandchild.Process.Pid)

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			// A bare select{} triggers Go's deadlock detector, unlike Node's
			// event loop. Keep the fixture alive until the transport kills it.
			for {
				time.Sleep(time.Hour)
			}
		}
		line = trimLine(line)
		if line == "" {
			continue
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal([]byte(line), &message) != nil || message.Method != "initialize" {
			continue
		}
		// The reference answers every initialize, with or without an id, and
		// writes `{jsonrpc, id, result}` in that order with the id omitted when
		// the request had none. A Go map cannot carry that order, so the line is
		// built explicitly.
		writeLine(message.ID)
	}
}

// writeLine emits one initialize response exactly as the reference's
// `JSON.stringify({ jsonrpc: "2.0", id: message.id, result })` does: id omitted
// when the request had none, then the reference's key order.
func writeLine(id json.RawMessage) {
	var buffer bytes.Buffer
	buffer.WriteString(`{"jsonrpc":"2.0"`)
	if len(id) > 0 {
		buffer.WriteString(`,"id":`)
		buffer.Write(id)
	}
	buffer.WriteString(`,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"stubborn-fixture","version":"1.0.0"}}}` + "\n")
	_, _ = os.Stdout.Write(buffer.Bytes())
}

func trimLine(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// stubbornfixture is the Go port of
// packages/mcp/test/fixtures/stubborn-server.mjs: answers initialize, spawns
// a grandchild that outlives stdin, and ignores stdin EOF and SIGTERM.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
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
	encoder := json.NewEncoder(os.Stdout)
	for {
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			select {} // stdin EOF ignored, like upstream
		}
		line = trimLine(line)
		if line == "" {
			continue
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal([]byte(line), &message) != nil || message.ID == nil {
			continue
		}
		if message.Method != "initialize" {
			continue
		}
		_ = encoder.Encode(map[string]any{
			"jsonrpc": "2.0", "id": message.ID,
			"result": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "stubborn-fixture", "version": "1.0.0"},
			},
		})
	}
}

func trimLine(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

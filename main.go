// Command pier is the Go port's interactive coding-agent CLI.
//
// It is a thin wrapper: the CLI itself lives in the cmd package (boot,
// flags, session resolution, run), so the module root is installable —
// `go build -o bin/pier .` — the layout github.com/dat267/min uses. Releases are
// GitHub release assets (`pier update`), not `go install ...@latest`, which would
// publish this module to proxy.golang.org.
package main

import "github.com/dat267/pier/cmd"

func main() {
	cmd.Execute()
}

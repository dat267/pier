package cmd

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Upstream main.ts reports a run failure as a red "Error: {message}" line and
// session-resolution failures as a red message without that prefix
// (main.ts:386/410 at f9a26365); file-processor.ts prints its own
// "Error: File not found" line. Pier prefixed every run failure with the binary
// name and printed it uncolored, and @file failures carried a second prefix.
func TestRunFailuresUseTheReferenceErrorLine(t *testing.T) {
	cases := []struct {
		name     string
		args     string
		wantLine string
		wantANSI bool
	}{
		{
			name:     "run failure",
			args:     "--offline --provider anthropic -p hi",
			wantLine: "Error: --provider requires --model (for example: --provider anthropic --model <pattern>)",
		},
		{
			name:     "colored run failure",
			args:     "--offline --provider anthropic -p hi",
			wantLine: "Error: --provider requires --model (for example: --provider anthropic --model <pattern>)",
			wantANSI: true,
		},
		{
			name:     "@file failure has one prefix",
			args:     "--offline -p hi @/nonexistent-file.txt",
			wantLine: "Error: File not found: /nonexistent-file.txt",
		},
		{
			name:     "session resolution is unprefixed",
			args:     "--offline --session abcdef",
			wantLine: "No session found matching 'abcdef'",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isolatedAgentDir(t)
			ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunFailureCLIHelper$")
			env := []string{
				"PIER_TEST_RUN_FAILURE_ARGS=" + testCase.args,
				"HOME=" + os.Getenv("HOME"),
				"TERM=xterm-256color",
			}
			if testCase.wantANSI {
				env = append(env, "FORCE_COLOR=1")
			}
			command.Env = env
			output, err := command.CombinedOutput()
			if exit := exitCodeOf(err); exit != 1 {
				t.Fatalf("exit = %d (%v), want 1; output: %q", exit, err, output)
			}
			text := string(output)
			if testCase.wantANSI {
				if !strings.Contains(text, "\x1b[31m"+testCase.wantLine+"\x1b[39m") {
					t.Fatalf("missing colored line %q in %q", testCase.wantLine, text)
				}
				return
			}
			if strings.ContainsAny(text, "\x1b") {
				t.Fatalf("escape codes in redirected output: %q", text)
			}
			for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
				if line == testCase.wantLine {
					return
				}
			}
			t.Fatalf("missing line %q in %q", testCase.wantLine, text)
		})
	}
}

func TestRunFailureCLIHelper(t *testing.T) {
	args, ok := os.LookupEnv("PIER_TEST_RUN_FAILURE_ARGS")
	if !ok {
		return
	}
	os.Args = append([]string{"pier"}, strings.Fields(args)...)
	Execute()
}

package offloop

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A panic on a queue worker never reaches the owner loop's recovery, so without
// a reporter the crash log stays empty and the failure is undiagnosable. The
// reporter must run with the panicking goroutine's stack before the process
// dies, and the panic must stay fatal (upstream's uncaught exception ends the
// process).
func TestQueueReportsWorkerPanicBeforeDying(t *testing.T) {
	const probeEnv = "PIER_TEST_OFFLOOP_PANIC"
	if os.Getenv(probeEnv) == "1" {
		q := New()
		q.SetPanicReporter(func(err error) {
			stacked, ok := err.(interface{ StackTrace() string })
			if !ok {
				os.Stderr.WriteString("no stack on " + err.Error() + "\n")
				return
			}
			os.Stderr.WriteString("REPORTED " + err.Error() + "\n" + stacked.StackTrace())
		})
		q.Go(func() { panic("worker boom") })
		select {}
	}
	command := exec.Command(os.Args[0], "-test.run=^TestQueueReportsWorkerPanicBeforeDying$")
	command.Env = append(os.Environ(), probeEnv+"=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("the worker panic did not end the process")
	}
	text := string(output)
	if !strings.Contains(text, "REPORTED worker boom") {
		t.Fatalf("panic was not reported:\n%s", text)
	}
	if !strings.Contains(text, "runReportedPanic") {
		t.Fatalf("reported panic has no worker stack:\n%s", text)
	}
	if !strings.Contains(text, "panic: worker boom") {
		t.Fatalf("panic was not re-raised:\n%s", text)
	}
}

// A reporter that panics must not hide the original worker panic.
func TestQueueSurvivesAPanickingReporter(t *testing.T) {
	const probeEnv = "PIER_TEST_OFFLOOP_PANIC_REPORTER"
	if os.Getenv(probeEnv) == "1" {
		q := New()
		q.SetPanicReporter(func(error) { panic("reporter boom") })
		q.Go(func() { panic("worker boom") })
		select {}
	}
	command := exec.Command(os.Args[0], "-test.run=^TestQueueSurvivesAPanickingReporter$")
	command.Env = append(os.Environ(), probeEnv+"=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("the worker panic did not end the process")
	}
	if !strings.Contains(string(output), "panic: worker boom") {
		t.Fatalf("original panic was replaced:\n%s", output)
	}
}

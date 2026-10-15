package e2e

import (
	"os"
	"testing"
)

// TestMain doubles the test binary as the fake engine: when launched with the
// fake engine's --params-file argument it serves the contract surface instead
// of running tests. argv (not env) is the trigger because Studio's managed
// engine start inherits a plain environment.
func TestMain(m *testing.M) {
	args := os.Args[1:]
	for i, a := range args {
		if a == "--params-file" && i+1 < len(args) {
			serveFakeEngine(args[i+1])
			return
		}
	}
	os.Exit(m.Run())
}

package e2e

import (
	"os"
	"testing"
)

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

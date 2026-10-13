package wizard

import (
	"strings"
	"testing"
)

func TestSuggestInitDir(t *testing.T) {
	if got := suggestInitDir("/home/u/.inferyard", false, false); got != "/home/u/.inferyard" {
		t.Fatalf("missing dir must be used as-is: %q", got)
	}
	nested := suggestInitDir("/home/u/.inferyard", true, false)
	if !strings.HasPrefix(nested, "/home/u/.inferyard/workspace-") {
		t.Fatalf("existing plain dir must suggest a fresh child: %q", nested)
	}
	if got := suggestInitDir("/home/u/.inferyard/workspace-1", true, true); got != "/home/u/.inferyard/workspace-1" {
		t.Fatalf("initialized workspace must never be nested: %q", got)
	}
}

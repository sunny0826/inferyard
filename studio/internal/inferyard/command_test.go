package inferyard

import (
	"reflect"
	"testing"
)

func TestSplitCommandWithoutShell(t *testing.T) {
	args, err := SplitCommand(`inferyard config create --workspace '/some space/work' --engine 'it'\''s engine'`)
	if err != nil || !reflect.DeepEqual(args, []string{"config", "create", "--workspace", "/some space/work", "--engine", "it's engine"}) {
		t.Fatalf("%q %v", args, err)
	}
	for _, command := range []string{"inferyard run ; true", "inferyard run $(evil)", "inferyard run | evil", "inferyard run `evil`", "inferyard run 'unclosed", "inferyard run\ntrue"} {
		if _, err := SplitCommand(command); err == nil {
			t.Fatal(command)
		}
	}
}

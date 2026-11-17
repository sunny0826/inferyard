package pages

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
)

// accessNode is the part of one node of MyGo's accessibility tree a screen
// reader receives for a control: its role, name and value.
type accessNode struct {
	role         uint64
	label, value string
}

// accessTree turns assistive technology on for a headless window and returns
// the tree MyGo hands the platform layer, the same one the macOS, Windows and
// Linux bridges export. The pinned MyGo (v0.7.4) gives Tester no public reader
// for it, so this reads the Tester's own fields; a MyGo upgrade that renames
// them fails here instead of passing silently.
func accessTree(t *testing.T, tt *ui.Tester) []accessNode {
	t.Helper()
	tester := reflect.ValueOf(tt).Elem()
	runtime := tester.FieldByName("rt")
	if !runtime.IsValid() || runtime.IsNil() {
		t.Fatal("MyGo Tester has no runtime field; update accessTree for the new MyGo")
	}
	enabled := runtime.Elem().FieldByName("access")
	if !enabled.IsValid() || enabled.Kind() != reflect.Bool {
		t.Fatal("MyGo runtime has no access switch; update accessTree for the new MyGo")
	}
	reflect.NewAt(enabled.Type(), unsafe.Pointer(enabled.UnsafeAddr())).Elem().SetBool(true)
	tt.Frame()
	host := tester.FieldByName("h")
	if !host.IsValid() || host.IsNil() {
		t.Fatal("MyGo Tester has no headless host; update accessTree for the new MyGo")
	}
	tree := host.Elem().FieldByName("access")
	if !tree.IsValid() || tree.IsNil() {
		t.Fatal("no accessibility tree after turning assistive technology on")
	}
	nodes := tree.Elem().FieldByName("Nodes")
	out := make([]accessNode, nodes.Len())
	for i := range out {
		n := nodes.Index(i)
		out[i] = accessNode{role: n.FieldByName("Role").Uint(), label: n.FieldByName("Label").String(), value: n.FieldByName("Value").String()}
	}
	return out
}

// menuButton returns the exported dropdown trigger named name.
func menuButton(t *testing.T, tree []accessNode, name string) accessNode {
	t.Helper()
	// Platform roles start at RoleGroup; RoleAuto and RoleNone never export.
	role := uint64(ui.RoleMenuButton - ui.RoleGroup)
	var named []string
	for _, n := range tree {
		if n.role == role {
			if n.label == name {
				return n
			}
			named = append(named, n.label)
		}
	}
	t.Fatalf("no dropdown named %q; dropdowns: %q", name, named)
	return accessNode{}
}

// A dropdown is found by its field name and still reads out the chosen option:
// the task page's engine and model pickers report the field as their name and
// the current option as their value, and the value follows a new choice.
func TestTaskSelectsExportFieldNameAndCurrentOption(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	tree := accessTree(t, tt)
	if engine := menuButton(t, tree, termCandidateEngine); engine.value != "engine-2 · llama-server" {
		t.Fatalf("engine dropdown value = %q", engine.value)
	}
	if model := menuButton(t, tree, "测评模型"); model.value != "请选择模型" {
		t.Fatalf("model dropdown value = %q", model.value)
	}
	click(t, tt, "测评模型")
	if err := tt.ChooseMenuItem("model.gguf · GGUF"); err != nil {
		t.Fatal(err)
	}
	if model := menuButton(t, accessTree(t, tt), "测评模型"); model.value != "model.gguf · GGUF" {
		t.Fatalf("model dropdown value after choosing = %q", model.value)
	}
}

// The long-context speed pickers share the same contract.
func TestSpeedSelectsExportFieldNameAndCurrentOption(t *testing.T) {
	s := testState()
	s.speed = &speedWorkspace{selected: true, context: inferyard.ContextSpeed128K}
	tt := ui.NewTester(func(c *ui.Context) { s.speedSelection(c) }, 1000, 700)
	tree := accessTree(t, tt)
	if kind := menuButton(t, tree, "测评类型"); kind.value != "长上下文速度" {
		t.Fatalf("kind dropdown value = %q", kind.value)
	}
	if context := menuButton(t, tree, "上下文档位"); context.value != "128k" {
		t.Fatalf("context dropdown value = %q", context.value)
	}
}

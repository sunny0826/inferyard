package pages

import (
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// accessNode is the part of one node of MyGo's accessibility tree a screen
// reader receives for a control: its parent index, role, name and value.
type accessNode struct {
	parent       int
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
		out[i] = accessNode{
			parent: int(n.FieldByName("Parent").Int()),
			role:   n.FieldByName("Role").Uint(),
			label:  n.FieldByName("Label").String(),
			value:  n.FieldByName("Value").String(),
		}
	}
	return out
}

// menuButton returns the exported native dropdown trigger named name.
func menuButton(t *testing.T, tree []accessNode, name string) accessNode {
	t.Helper()
	return controlByRole(t, tree, uint64(ui.RoleMenuButton-ui.RoleGroup), name)
}

// popUpButton returns the exported custom select trigger named name.
func popUpButton(t *testing.T, tree []accessNode, name string) accessNode {
	t.Helper()
	return controlByRole(t, tree, uint64(ui.RolePopUpButton-ui.RoleGroup), name)
}

func controlByRole(t *testing.T, tree []accessNode, role uint64, name string) accessNode {
	t.Helper()
	// Platform roles start at RoleGroup; RoleAuto and RoleNone never export.
	var named []string
	for _, n := range tree {
		if n.role == role {
			if n.label == name {
				return n
			}
			named = append(named, n.label)
		}
	}
	t.Fatalf("no control with role %d named %q; found: %q", role, name, named)
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
	var active bool
	tt := ui.NewTester(func(c *ui.Context) { active = s.speedSelection(c) }, 1000, 700)
	if !active {
		t.Fatal("speed mode did not stay selected")
	}
	tree := accessTree(t, tt)
	if kind := menuButton(t, tree, "测评类型"); kind.value != "长上下文速度" {
		t.Fatalf("kind dropdown value = %q", kind.value)
	}
	if context := menuButton(t, tree, "上下文档位"); context.value != "128k" {
		t.Fatalf("context dropdown value = %q", context.value)
	}
}

// Download task cards export separate RoleGroup containers named after each
// task's output directory, so repeated "观测依据与输出" folds stay scoped to their
// own card instead of flattening under the page container.
func TestDownloadTaskCardsExportDistinctAccessibleGroups(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{
		{TaskId: "dl-1", Kind: "model", State: "failed", OutName: "model", OutDir: "/models/model", Error: core.Err("download_failed")},
		{TaskId: "dl-2", Kind: "model", State: "done", OutName: "model-2", OutDir: "/models/model-2"},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1024, 681)
	tree := accessTree(t, tt)
	groupRole := uint64(ui.RoleGroup - ui.RoleGroup)
	groups := map[string]int{}
	for i, n := range tree {
		if n.role == groupRole && (n.label == "model" || n.label == "model-2") {
			groups[n.label] = i
		}
	}
	if len(groups) != 2 || groups["model"] == groups["model-2"] {
		t.Errorf("card RoleGroups = %v, want distinct groups for model and model-2", groups)
	}
	owners := disclosureGroupOwners(tree, "观测依据与输出")
	if len(groups) != 2 || !slices.Equal(owners, []int{groups["model"], groups["model-2"]}) {
		t.Errorf("disclosure group owners = %v, want distinct card groups %v", owners, groups)
	}
}

func disclosureGroupOwners(tree []accessNode, label string) []int {
	disclosureRole := uint64(ui.RoleDisclosure - ui.RoleGroup)
	groupRole := uint64(ui.RoleGroup - ui.RoleGroup)
	var owners []int
	for _, n := range tree {
		if n.role != disclosureRole || n.label != label {
			continue
		}
		owner := -1
		for p := n.parent; p >= 0; p = tree[p].parent {
			if tree[p].role == groupRole && tree[p].label != "" {
				owner = p
				break
			}
		}
		owners = append(owners, owner)
	}
	return owners
}

// Custom Select triggers (RolePopUpButton) export their fixed field label as
// the accessible name and the human-readable option label — never the internal
// JSON key or slice index — as the accessible value.
func TestPopUpSelectsExportFieldNameAndCurrentOption(t *testing.T) {
	s := testState()
	chartTester := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, 1120, 2300)
	if got := popUpButton(t, accessTree(t, chartTester), "传感器温度 °C来源").value; got != "AppleSMC:Tg0C" {
		t.Errorf("initial temperature source value = %q, want %q", got, "AppleSMC:Tg0C")
	}
	click(t, chartTester, "传感器温度 °C来源")
	click(t, chartTester, "AppleSMC:Tp0C")
	if got := popUpButton(t, accessTree(t, chartTester), "传感器温度 °C来源").value; got != "AppleSMC:Tp0C" {
		t.Errorf("selected temperature source value = %q, want %q", got, "AppleSMC:Tp0C")
	}

	runs := []core.ReportRun{
		{Facets: inferyard.StudioFacets{BundleLabel: "zh-core"}, Source: inferyard.StudioSource{RunID: "run-1"}},
		{Facets: inferyard.StudioFacets{BundleLabel: "zh-svg-pelican"}, Source: inferyard.StudioSource{RunID: "run-2"}},
	}
	rv := reportViewState{index: &core.ReportIndex{Runs: runs}}
	reportTester := ui.NewTester(func(c *ui.Context) { rv.runPicker(c) }, 900, 400)
	if got, want := popUpButton(t, accessTree(t, reportTester), "选择运行").value, reportRunLabel(runs[0]); got != want {
		t.Errorf("initial run picker value = %q, want %q", got, want)
	}
	click(t, reportTester, "选择运行")
	click(t, reportTester, reportRunLabel(runs[1]))
	if got, want := popUpButton(t, accessTree(t, reportTester), "选择运行").value, reportRunLabel(runs[1]); got != want {
		t.Errorf("selected run picker value = %q, want %q", got, want)
	}
}

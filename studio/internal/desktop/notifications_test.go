package desktop

import (
	"encoding/json"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestNotificationsLocalized(t *testing.T) {
	runDone := core.Event{
		Type: "run.state",
		Data: json.RawMessage(`{"task":{"kind":"context_speed","state":"done"}}`),
	}
	reportReady := core.Event{
		Type: "report.created",
		Data: json.RawMessage(`{"report":{"report_id":"rep-1"}}`),
	}

	restoreEn := l10n.SetTestLocale(l10n.LanguageEn)
	msg, ok := messageFor(runDone)
	if !ok || !strings.Contains(msg.Title, "Long-context speed benchmark") {
		restoreEn()
		t.Fatalf("expected English run notification, got ok=%v msg=%+v", ok, msg)
	}
	rep, ok := messageFor(reportReady)
	if !ok || rep.Title != "Report Ready" || !strings.Contains(rep.Body, "rep-1") {
		restoreEn()
		t.Fatalf("expected English report notification, got ok=%v msg=%+v", ok, rep)
	}
	restoreEn()

	restoreZh := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restoreZh()
	msg, ok = messageFor(runDone)
	if !ok || !strings.Contains(msg.Title, "长上下文专项") {
		t.Fatalf("expected Chinese run notification, got ok=%v msg=%+v", ok, msg)
	}
	rep, ok = messageFor(reportReady)
	if !ok || rep.Title != "报告已生成" || !strings.Contains(rep.Body, "rep-1") {
		t.Fatalf("expected Chinese report notification, got ok=%v msg=%+v", ok, rep)
	}
}

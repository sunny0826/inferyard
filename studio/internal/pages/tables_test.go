package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

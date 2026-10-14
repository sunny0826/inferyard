package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

type pageTableState struct {
	layout      components.TableState
	Selected    *int
	Key         func(int) any
	Label       func(int) string
	selectedKey any
	focus       ui.Handle
	submitted   bool
}

func (s *pageTableState) Focus(c *ui.Context) { s.focus.Focus(c) }

func pageTable(c *ui.Context, s *pageTableState, label string, columns []ui.TableColumn, count int, cell func(int, int)) ui.Element {
	s.submitted = false
	if s.Selected == nil {
		index := -1
		s.Selected = &index
	}
	if s.selectedKey != nil {
		*s.Selected = -1
		for i := range count {
			if s.Key(i) == s.selectedKey {
				*s.Selected = i
				break
			}
		}
	}
	return components.ScrollView(c, components.ScrollViewOptions{Axes: components.ScrollBoth}).FillWidth().MinWidth(0).Children(func() {
		rows := make([]ui.Element, count)
		components.Table(c, &s.layout, columns, func() {
			for i := range count {
				key := s.Key(i)
				rowLabel := fmt.Sprint(key)
				if s.Label != nil {
					rowLabel = s.Label(i)
				}
				row := components.TableRow(c, key, rowLabel, *s.Selected == i, func(col int) { cell(i, col) })
				rows[i] = row
				submit := row.Shortcut(0, ui.KeyEnter) || row.DoubleClicked()
				if row.Clicked() || row.Focused() || submit {
					*s.Selected, s.selectedKey = i, key
					row.Selected(true)
				}
				if submit {
					s.submitted = true
				}
			}
		}).Shrink(0).Label(label)
		if i := selectedRow(s, count); i >= 0 {
			rows[i].Bind(&s.focus)
		}
	})
}

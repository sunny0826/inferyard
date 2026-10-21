package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// pickerOption is one row of a searchable select. Disabled options stay
// visible with their reason but cannot be chosen, by pointer or keyboard.
type pickerOption struct {
	value, note string
	// label is the text shown for the option. It defaults to the value.
	label    string
	disabled bool
}

// display returns the text shown for the option.
func (o pickerOption) display() string {
	if o.label != "" {
		return o.label
	}
	return o.value
}

// pickerSelect renders a onevra Select with a search field and annotated
// options, following the models/downloads prototype pickers. It reports a
// changed selection; the caller owns side effects.
func pickerSelect(c *ui.Context, label, placeholder string, selected *string, query *string, options []pickerOption, emptyMessage string) bool {
	t := c.Theme()
	sel := components.Select(c, selected)
	sel.Trigger.Label(label).Children(func() {
		value := *selected
		if value == "" {
			value = placeholder
		} else {
			for _, opt := range options {
				if opt.value == value {
					value = opt.display()
					break
				}
			}
		}
		components.Text(c, value).SingleLine()
	})
	sel.Popup(func(panel ui.Element) {
		sel.SearchField(query).Label("搜索" + label).Placeholder("输入" + label + "名称")
		shown := 0
		q := strings.ToLower(strings.TrimSpace(*query))
		for _, opt := range options {
			if q != "" && !strings.Contains(strings.ToLower(opt.display()), q) {
				continue
			}
			shown++
			row := func() {
				ui.Column(c).Gap(t.Space(0.5)).Children(func() {
					ui.Text(c, opt.display())
					if opt.note != "" {
						ui.Text(c, opt.note).FontSize(max(12, t.Rem(.85))).TextColor(t.TextMuted)
					}
				})
			}
			if opt.disabled {
				ui.Column(c).Padding(t.Space(1.5), t.Space(2.5)).Children(func() {
					ui.Column(c).Gap(t.Space(0.5)).Children(func() {
						ui.Text(c, opt.display()).TextColor(t.TextMuted)
						if opt.note != "" {
							ui.Text(c, opt.note).FontSize(max(12, t.Rem(.85))).TextColor(t.TextMuted)
						}
					})
				})
				continue
			}
			sel.Item(opt.value).Children(row)
		}
		if shown == 0 {
			ui.Text(c, emptyMessage).TextColor(t.TextMuted)
		}
	})
	if sel.Trigger.Changed() {
		*query = ""
		return true
	}
	return false
}

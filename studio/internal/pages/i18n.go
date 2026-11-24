package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/l10n"
)

func contextLocale(c *ui.Context) string {
	if c != nil {
		if loc := c.Locale(); loc != "" {
			return loc
		}
	}
	return l10n.CurrentLocale()
}

func tr(c *ui.Context, key string) string {
	return l10n.T(contextLocale(c), key)
}

func trf(c *ui.Context, key string, kv ...any) string {
	return l10n.Tf(contextLocale(c), key, kv...)
}

func syncNativeLocale(setting string) {
	mygo.App.SetLocale(l10n.LocaleForSetting(setting))
}

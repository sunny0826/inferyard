package pages

import "github.com/egoist/mygo/ui"

var candidateTokenStyle func(*ui.Context, designTokens) designTokens

func styledTokens(c *ui.Context, base designTokens) designTokens {
	if candidateTokenStyle == nil {
		return base
	}
	return candidateTokenStyle(c, base)
}

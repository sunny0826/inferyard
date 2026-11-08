package pages

import "github.com/egoist/mygo/ui"

// candidateTokenStyle lets a debug build install its preview-only candidate
// tokens. Production builds leave it nil and always resolve the defaults; it
// holds no preview implementation itself.
var candidateTokenStyle func(*ui.Context, designTokens) designTokens

// styledTokens returns the shared tokens, or the active preview candidate set.
func styledTokens(c *ui.Context, base designTokens) designTokens {
	if candidateTokenStyle == nil {
		return base
	}
	return candidateTokenStyle(c, base)
}

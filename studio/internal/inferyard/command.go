package inferyard

import (
	"errors"
	"strings"
	"unicode"
)

// SplitCommand handles quoted CLI arguments without evaluating a shell.
func SplitCommand(command string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range command {
		if r == 0 || r == '\n' || r == '\r' {
			return nil, errors.New("invalid command")
		}
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		if strings.ContainsRune(";&|`$<>", r) {
			return nil, errors.New("shell syntax is not supported")
		}
		word.WriteRune(r)
		started = true
	}
	if quote != 0 || escaped {
		return nil, errors.New("unclosed quote")
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) < 2 || args[0] != "inferyard" {
		return nil, errors.New("expected inferyard command")
	}
	return args[1:], nil
}

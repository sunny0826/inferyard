package remote

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type sshHostConfig struct {
	hostname     string
	user         string
	port         int
	identityFile string
	proxyJump    string
}

func ParseSSHCommand(raw string) (ParsedSSHCommand, error) {
	if strings.ContainsAny(raw, ";|&$`\n\r><\x00") {
		return ParsedSSHCommand{}, ErrInvalidSSHCommand
	}
	tokens, err := splitSSHWords(raw)
	if err != nil || len(tokens) < 2 || tokens[0] != "ssh" {
		return ParsedSSHCommand{}, ErrInvalidSSHCommand
	}
	parsed := ParsedSSHCommand{}
	var target string
	for index := 1; index < len(tokens); index++ {
		token := tokens[index]
		if target != "" || token == "" {
			return ParsedSSHCommand{}, ErrInvalidSSHCommand
		}
		if token == "--" {
			index++
			if index >= len(tokens) {
				return ParsedSSHCommand{}, ErrInvalidSSHCommand
			}
			target = tokens[index]
			if index != len(tokens)-1 {
				return ParsedSSHCommand{}, ErrInvalidSSHCommand
			}
			break
		}
		if !strings.HasPrefix(token, "-") {
			target = token
			if index != len(tokens)-1 {
				return ParsedSSHCommand{}, ErrInvalidSSHCommand
			}
			break
		}
		name, value, hasValue := token[:2], "", false
		if len(token) > 2 {
			value, hasValue = token[2:], true
		}
		if name != "-p" && name != "-i" && name != "-J" && name != "-l" {
			return ParsedSSHCommand{}, ErrInvalidSSHCommand
		}
		if !hasValue {
			index++
			if index >= len(tokens) {
				return ParsedSSHCommand{}, ErrInvalidSSHCommand
			}
			value = tokens[index]
		}
		if value == "" {
			return ParsedSSHCommand{}, ErrInvalidSSHCommand
		}
		switch name {
		case "-p":
			port, err := parseSSHPort(value)
			if err != nil {
				return ParsedSSHCommand{}, err
			}
			parsed.Port = port
		case "-i":
			parsed.IdentityFile = value
		case "-J":
			parsed.ProxyJump = value
		case "-l":
			parsed.User = value
		}
	}
	if target == "" {
		return ParsedSSHCommand{}, ErrInvalidSSHCommand
	}
	parsed.RawTarget = target
	if strings.HasPrefix(target, "ssh://") {
		if err := parseSSHURL(target, &parsed); err != nil {
			return ParsedSSHCommand{}, err
		}
	} else if at := strings.LastIndex(target, "@"); at >= 0 {
		if parsed.User == "" {
			parsed.User = target[:at]
		}
		parsed.Host = target[at+1:]
	} else {
		parsed.Host = target
	}
	parsed.Host = strings.Trim(parsed.Host, "[]")
	home, _ := os.UserHomeDir()
	config, _ := readSSHHostConfig(filepath.Join(home, ".ssh", "config"), parsed.RawTarget)
	if config.hostname != "" {
		parsed.Host = config.hostname
	}
	if parsed.User == "" {
		parsed.User = config.user
	}
	if parsed.Port == 0 {
		parsed.Port = config.port
	}
	if parsed.IdentityFile == "" {
		parsed.IdentityFile = config.identityFile
	}
	if parsed.ProxyJump == "" {
		parsed.ProxyJump = config.proxyJump
	}
	if parsed.Port == 0 {
		parsed.Port = 22
	}
	if !validSSHIdentityPart(parsed.User) || !validSSHHost(parsed.Host) || !validSSHOption(parsed.IdentityFile) || !validSSHOption(parsed.ProxyJump) {
		return ParsedSSHCommand{}, ErrInvalidSSHCommand
	}
	if strings.HasPrefix(parsed.IdentityFile, "~") {
		home, _ := os.UserHomeDir()
		parsed.IdentityFile = filepath.Join(home, strings.TrimPrefix(parsed.IdentityFile, "~/"))
	}
	return parsed, nil
}

func (h *hub) ParseSSHCommand(raw string) (ParsedSSHCommand, error) { return ParseSSHCommand(raw) }

func splitSSHWords(raw string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for _, r := range strings.TrimSpace(raw) {
		if escaped {
			word.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
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
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		word.WriteRune(r)
	}
	if quote != 0 || escaped {
		return nil, ErrInvalidSSHCommand
	}
	flush()
	return words, nil
}

func parseSSHURL(target string, parsed *ParsedSSHCommand) error {
	parsedURL, err := url.Parse(target)
	if err != nil || parsedURL.Scheme != "ssh" || parsedURL.Host == "" || parsedURL.Path != "" || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return ErrInvalidSSHCommand
	}
	parsed.Host = parsedURL.Hostname()
	if parsed.User == "" && parsedURL.User != nil {
		parsed.User = parsedURL.User.Username()
	}
	if parsedURL.User != nil {
		if _, set := parsedURL.User.Password(); set {
			return ErrInvalidSSHCommand
		}
	}
	if rawPort := parsedURL.Port(); rawPort != "" {
		port, err := parseSSHPort(rawPort)
		if err != nil {
			return err
		}
		parsed.Port = port
	}
	return nil
}

func parseSSHPort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, ErrInvalidSSHCommand
	}
	return port, nil
}

func readSSHHostConfig(configPath, target string) (sshHostConfig, error) {
	var result sshHostConfig
	file, err := os.Open(configPath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	alias := target
	if at := strings.LastIndex(alias, "@"); at >= 0 {
		alias = alias[at+1:]
	}
	var active bool
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		keyword := strings.ToLower(fields[0])
		if keyword == "host" {
			active = false
			for _, pattern := range fields[1:] {
				matched, _ := path.Match(pattern, alias)
				if matched {
					active = true
				}
			}
			continue
		}
		if !active {
			continue
		}
		value := strings.Join(fields[1:], " ")
		switch keyword {
		case "hostname":
			if result.hostname == "" {
				result.hostname = value
			}
		case "user":
			if result.user == "" {
				result.user = value
			}
		case "port":
			if result.port == 0 {
				result.port, _ = strconv.Atoi(value)
			}
		case "identityfile":
			if result.identityFile == "" {
				result.identityFile = value
			}
		case "proxyjump":
			if result.proxyJump == "" {
				result.proxyJump = value
			}
		}
	}
	return result, scanner.Err()
}

func validSSHIdentityPart(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && validSSHOption(value)
}

func validSSHHost(value string) bool {
	return value != "" && (net.ParseIP(value) != nil || validDNSName(value)) && validSSHOption(value)
}

func validSSHOption(value string) bool {
	return !strings.ContainsAny(value, "\r\n\x00;|&$`><")
}

func sshAddress(host string, port int) string { return net.JoinHostPort(host, fmt.Sprint(port)) }

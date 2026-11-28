package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSSHCommandVariants(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		user     string
		host     string
		port     int
		identity string
		jump     string
	}{
		{name: "cloud", command: "ssh -p 38291 root@connect.westb.seetacloud.com", user: "root", host: "connect.westb.seetacloud.com", port: 38291},
		{name: "options", command: `ssh -i "/tmp/key file" -J admin@jump.example.com -l ubuntu -p2200 gpu.example.com`, user: "ubuntu", host: "gpu.example.com", port: 2200, identity: "/tmp/key file", jump: "admin@jump.example.com"},
		{name: "url", command: "ssh ssh://root@example.com:2222", user: "root", host: "example.com", port: 2222},
		{name: "compact", command: "ssh -p22 -lroot -i/tmp/id -Jjump.example.com host.example.com", user: "root", host: "host.example.com", port: 22, identity: "/tmp/id", jump: "jump.example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseSSHCommand(test.command)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.User != test.user || parsed.Host != test.host || parsed.Port != test.port ||
				parsed.IdentityFile != test.identity || parsed.ProxyJump != test.jump {
				t.Fatalf("parsed = %+v", parsed)
			}
		})
	}
}

func TestParseSSHCommandUsesSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "Host cloud-gpu\n  HostName connect.example.com\n  User root\n  Port 39222\n  IdentityFile ~/.ssh/cloud_key\n  ProxyJump admin@jump.example.com\nHost *\n  User fallback\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSSHCommand("ssh cloud-gpu")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "connect.example.com" || parsed.User != "root" || parsed.Port != 39222 ||
		parsed.IdentityFile != filepath.Join(home, ".ssh", "cloud_key") || parsed.ProxyJump != "admin@jump.example.com" {
		t.Fatalf("parsed config = %+v", parsed)
	}
	explicit, err := ParseSSHCommand("ssh -p 22 -l explicit cloud-gpu")
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Port != 22 || explicit.User != "explicit" {
		t.Fatalf("explicit options lost: %+v", explicit)
	}
}

func TestParseSSHCommandRejectsInjectionAndRemoteCommands(t *testing.T) {
	commands := []string{
		"ssh root@example.com; touch /tmp/pwned", "ssh root@example.com | sh", "ssh root@example.com && sh",
		"ssh root@example.com$(id)", "ssh root@example.com`id`", "ssh root@example.com\nwhoami",
		"ssh root@example.com > /tmp/out", "ssh root@example.com whoami", "ssh -oProxyCommand=sh root@example.com",
		"ssh -p nope root@example.com", "ssh", "ssh ssh://root:password@example.com",
	}
	for _, command := range commands {
		t.Run(strings.ReplaceAll(command, "/", "_"), func(t *testing.T) {
			if _, err := ParseSSHCommand(command); !errors.Is(err, ErrInvalidSSHCommand) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

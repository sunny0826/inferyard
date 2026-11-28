package remote

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

func (h *hub) installManagedSSHKey(client *ssh.Client, nodeID string) error {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	keyDir := filepath.Join(h.cfg.DataDir, "ssh", nodeID)
	keyPath := filepath.Join(keyDir, "id_ed25519")
	if data, err := os.ReadFile(keyPath); err == nil {
		if parsed, parseErr := ssh.ParsePrivateKey(data); parseErr == nil {
			return installAuthorizedKey(client, parsed.PublicKey(), nodeID)
		}
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := writePrivateFile(keyPath, keyPEM); err != nil {
		return err
	}
	publicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		return err
	}
	return installAuthorizedKey(client, publicKey, nodeID)
}

func installAuthorizedKey(client *ssh.Client, publicKey ssh.PublicKey, nodeID string) error {
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))) + " inferyard-studio-" + nodeID
	command := "umask 077; mkdir -p \"$HOME/.ssh\"; chmod 700 \"$HOME/.ssh\"; touch \"$HOME/.ssh/authorized_keys\"; " +
		"grep -q -F " + shellQuote(authorized) + " \"$HOME/.ssh/authorized_keys\" 2>/dev/null || " +
		"printf '%s\\n' " + shellQuote(authorized) + " >> \"$HOME/.ssh/authorized_keys\"; chmod 600 \"$HOME/.ssh/authorized_keys\""
	_, err := runRemoteCommand(client, command, nil)
	return err
}

func deployRemoteAgent(client *ssh.Client, issued issuedClientCredentials, version, source, proxy string) error {
	directory := "$HOME/.inferyard/remote/" + issued.NodeID
	quotedDirectory := remoteHomeQuote(directory)
	command := "umask 077; mkdir -p " + quotedDirectory + "; chmod 700 " + quotedDirectory
	if _, err := runRemoteCommand(client, command, nil); err != nil {
		return fmt.Errorf("create remote credential directory: %w", err)
	}
	files := []struct {
		name string
		data []byte
	}{
		{"ca.pem", issued.CAPEM}, {"client.pem", issued.CertPEM}, {"client-key.pem", issued.KeyPEM},
	}
	for _, file := range files {
		path := remoteHomeQuote(directory + "/" + file.name)
		if _, err := runRemoteCommand(client, "umask 077; cat > "+path+"; chmod 600 "+path, file.data); err != nil {
			return fmt.Errorf("write remote %s: %w", file.name, err)
		}
	}
	install := unixUVBootstrap()
	if source != "" {
		install += "export UV_DEFAULT_INDEX=" + shellQuote(source) + "; "
	}
	if proxy != "" {
		install += "export HTTPS_PROXY=" + shellQuote(proxy) + " HTTP_PROXY=" + shellQuote(proxy) + "; "
	}
	install += "\"$UV\" tool install --python 3.15 --upgrade " + shellQuote(packageSpec(version))
	if _, err := runRemoteCommand(client, install, nil); err != nil {
		return fmt.Errorf("deploy InferYard CLI: %w", err)
	}
	return nil
}

func startRemoteAgent(client *ssh.Client, nodeID string, reversePort int, version string) error {
	directory := "$HOME/.inferyard/remote/" + nodeID
	uv := "\"$HOME/.local/share/inferyard/bin/uv\""
	command := "UV=" + uv + "; [ -x \"$UV\" ] || UV=$(command -v uv); " +
		"nohup \"$UV\" tool run --from " + shellQuote(packageSpec(version)) + " inferyard agent run " +
		fmt.Sprintf("--hub 127.0.0.1:%d ", reversePort) +
		"--ca-cert " + remoteHomeQuote(directory+"/ca.pem") + " " +
		"--client-cert " + remoteHomeQuote(directory+"/client.pem") + " " +
		"--client-key " + remoteHomeQuote(directory+"/client-key.pem") + " " +
		"> " + remoteHomeQuote(directory+"/agent.log") + " 2>&1 < /dev/null &"
	_, err := runRemoteCommand(client, command, nil)
	if err != nil {
		return fmt.Errorf("start remote InferYard agent: %w", err)
	}
	return nil
}

func remoteHomeQuote(path string) string {
	return "\"" + path + "\""
}

func runRemoteCommand(client *ssh.Client, command string, input []byte) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if input != nil {
		session.Stdin = bytes.NewReader(input)
	}
	output, err := session.CombinedOutput(command)
	if err != nil {
		return nil, fmt.Errorf("remote command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

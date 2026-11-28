package remote

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCAPersistsKeyAndRevocations(t *testing.T) {
	dataDir := t.TempDir()
	first, err := loadCertificateAuthority(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := first.sha256()
	issued, err := first.issueNode("node-test")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pemDecode(issued.CertPEM)
	cert, err := x509.ParseCertificate(block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.verifyPeer([]*x509.Certificate{cert}); err != nil {
		t.Fatal(err)
	}
	if err := first.revoke("node-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.verifyPeer([]*x509.Certificate{cert}); err == nil {
		t.Fatal("revoked certificate verified")
	}
	second, err := loadCertificateAuthority(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if second.sha256() != fingerprint {
		t.Fatal("CA changed after reload")
	}
	if _, err := second.verifyPeer([]*x509.Certificate{cert}); err == nil {
		t.Fatal("revocation was not persisted")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dataDir, "ca", "root-key.pem"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("key mode = %o", info.Mode().Perm())
		}
	}
}

func TestServerCertificateSANs(t *testing.T) {
	ca, err := loadCertificateAuthority(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := ca.serverCertificate([]string{"gpu.local", "100.100.1.2"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("gpu.local"); err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("100.100.1.2"); err != nil {
		t.Fatal(err)
	}
	if len(certificate.Certificate) != 2 {
		t.Fatal("server chain does not include root CA")
	}
}

func pemDecode(data []byte) ([]byte, []byte) {
	block, rest := pem.Decode(data)
	return block.Bytes, rest
}

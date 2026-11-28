package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type certificateAuthority struct {
	dir     string
	key     ed25519.PrivateKey
	cert    *x509.Certificate
	certPEM []byte
	pool    *x509.CertPool

	mu      sync.RWMutex
	revoked map[string]bool
}

type issuedClientCredentials struct {
	NodeID      string
	CertPEM     []byte
	KeyPEM      []byte
	CAPEM       []byte
	Serial      string
	Fingerprint string
}

func loadCertificateAuthority(dataDir string) (*certificateAuthority, error) {
	dir := filepath.Join(dataDir, "ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create CA directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	ca := &certificateAuthority{dir: dir, revoked: make(map[string]bool)}
	keyPath := filepath.Join(dir, "root-key.pem")
	certPath := filepath.Join(dir, "root-cert.pem")
	keyData, keyErr := os.ReadFile(keyPath)
	certData, certErr := os.ReadFile(certPath)
	if keyErr == nil && certErr == nil {
		if err := ca.load(keyData, certData); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(keyErr) || !os.IsNotExist(certErr) {
		return nil, fmt.Errorf("CA key and certificate must both exist")
	} else if err := ca.generate(); err != nil {
		return nil, err
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return nil, err
	}
	revokedData, err := os.ReadFile(filepath.Join(dir, "revoked.json"))
	if err == nil {
		var nodeIDs []string
		if err := json.Unmarshal(revokedData, &nodeIDs); err != nil {
			return nil, fmt.Errorf("decode revoked nodes: %w", err)
		}
		for _, nodeID := range nodeIDs {
			ca.revoked[nodeID] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return ca, nil
}

func (ca *certificateAuthority) generate() error {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "InferYard Studio Root CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return err
	}
	ca.key = privateKey
	ca.cert, err = x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	ca.certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	ca.pool = x509.NewCertPool()
	ca.pool.AddCert(ca.cert)
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writePrivateFile(filepath.Join(ca.dir, "root-key.pem"), keyPEM); err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(ca.dir, "root-cert.pem"), ca.certPEM)
}

func (ca *certificateAuthority) load(keyPEM, certPEM []byte) error {
	keyBlock, _ := pem.Decode(keyPEM)
	certBlock, _ := pem.Decode(certPEM)
	if keyBlock == nil || certBlock == nil {
		return fmt.Errorf("invalid CA PEM data")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA key: %w", err)
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return fmt.Errorf("CA key is not Ed25519")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA certificate: %w", err)
	}
	if !cert.IsCA || !privateKey.Public().(ed25519.PublicKey).Equal(cert.PublicKey) {
		return fmt.Errorf("CA key and certificate do not match")
	}
	ca.key = privateKey
	ca.cert = cert
	ca.certPEM = certPEM
	ca.pool = x509.NewCertPool()
	ca.pool.AddCert(cert)
	return nil
}

func (ca *certificateAuthority) sha256() string {
	digest := sha256.Sum256(ca.cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(digest[:])
}

func (ca *certificateAuthority) serverCertificate(hosts []string) (tls.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "InferYard Studio Hub"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	seen := map[string]bool{}
	for _, host := range append(slices.Clone(hosts), "localhost", "127.0.0.1", "::1") {
		host = strings.TrimSpace(strings.Trim(host, "[]"))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else if validDNSName(host) {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, privateKey.Public(), ca.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: privateKey}, nil
}

func (ca *certificateAuthority) issueNode(nodeID string) (issuedClientCredentials, error) {
	if !validNodeID(nodeID) {
		return issuedClientCredentials{}, fmt.Errorf("invalid node ID")
	}
	serial, err := randomSerial()
	if err != nil {
		return issuedClientCredentials{}, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return issuedClientCredentials{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: nodeID},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, publicKey, ca.key)
	if err != nil {
		return issuedClientCredentials{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return issuedClientCredentials{}, err
	}
	digest := sha256.Sum256(der)
	return issuedClientCredentials{
		NodeID: nodeID, CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), CAPEM: ca.certPEM,
		Serial: serial.String(), Fingerprint: hex.EncodeToString(digest[:]),
	}, nil
}

func (ca *certificateAuthority) verifyPeer(certs []*x509.Certificate) (string, error) {
	if len(certs) == 0 {
		return "", fmt.Errorf("client certificate required")
	}
	leaf := certs[0]
	nodeID := leaf.Subject.CommonName
	ca.mu.RLock()
	revoked := ca.revoked[nodeID]
	ca.mu.RUnlock()
	if revoked || !validNodeID(nodeID) {
		return "", fmt.Errorf("node certificate revoked or invalid")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: ca.pool, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return "", fmt.Errorf("verify node certificate: %w", err)
	}
	return nodeID, nil
}

func (ca *certificateAuthority) revoke(nodeID string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.revoked[nodeID] = true
	nodeIDs := make([]string, 0, len(ca.revoked))
	for id := range ca.revoked {
		nodeIDs = append(nodeIDs, id)
	}
	slices.Sort(nodeIDs)
	data, err := json.MarshalIndent(nodeIDs, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(ca.dir, "revoked.json"), append(data, '\n'))
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "/\\@: \t\r\n") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

func validNodeID(nodeID string) bool {
	if len(nodeID) < 1 || len(nodeID) > 128 {
		return false
	}
	for _, r := range nodeID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

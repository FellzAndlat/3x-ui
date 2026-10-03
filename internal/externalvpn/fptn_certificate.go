package externalvpn

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"time"
)

func newFPTNCertificate(host string) (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = []net.IP{ip}
	} else {
		cert.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), nil
}

func fptnCertificateFiles(inst Instance, folder string) (string, string, error) {
	s := inst.Settings
	var pair tls.Certificate
	var err error
	if s.Certificate != "" {
		pair, err = tls.LoadX509KeyPair(s.Certificate, s.PrivateKey)
	} else {
		pair, err = tls.X509KeyPair([]byte(s.CertificatePEM), []byte(s.PrivateKeyPEM))
	}
	if err != nil {
		return "", "", fmt.Errorf("FPTN automatic certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", "", err
	}
	if _, ok := pair.PrivateKey.(*rsa.PrivateKey); !ok {
		return "", "", fmt.Errorf("FPTN requires an RSA certificate for its RS256 access tokens")
	}
	if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return "", "", fmt.Errorf("FPTN certificate is outside its validity period")
	}
	if err := leaf.VerifyHostname(s.Hostname); err != nil {
		return "", "", err
	}
	if s.Certificate != "" {
		return s.Certificate, s.PrivateKey, nil
	}
	cert, key := filepath.Join(folder, "cert.pem"), filepath.Join(folder, "key.pem")
	if err := writePrivate(cert, []byte(s.CertificatePEM)); err != nil {
		return "", "", err
	}
	if err := writePrivate(key, []byte(s.PrivateKeyPEM)); err != nil {
		return "", "", err
	}
	return cert, key, nil
}

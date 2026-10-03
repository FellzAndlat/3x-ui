package youtubeproxy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type authority struct {
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	mu     sync.Mutex
	leaves map[string]*tls.Certificate
}

func serial() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }

var authorityLoadMu sync.Mutex

func loadAuthority(dir string) (*authority, error) {
	authorityLoadMu.Lock()
	defer authorityLoadMu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "ca-private.pem")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		number, err := serial()
		if err != nil {
			return nil, err
		}
		template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: "3x-ui YouTube server filter CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		private, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		data = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})...)
		// O_EXCL prevents concurrent startup from replacing a CA trusted by clients.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, err = file.Write(data)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("CA private file must have mode 0600: %s", path)
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || !cert.IsCA || time.Now().After(cert.NotAfter) {
		return nil, fmt.Errorf("invalid or expired filter CA")
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	publicPath := filepath.Join(dir, "ca-cert.pem")
	existing, _ := os.ReadFile(publicPath)
	if !bytes.Equal(existing, public) {
		if err = os.WriteFile(publicPath, public, 0644); err != nil {
			return nil, err
		}
	}
	return &authority{cert: cert, key: key, leaves: map[string]*tls.Certificate{}}, nil
}
func (a *authority) leaf(host string) (*tls.Certificate, error) {
	if !interceptedHost(host) {
		return nil, fmt.Errorf("host not allowed for interception")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cached := a.leaves[host]; cached != nil && time.Now().Before(cached.Leaf.NotAfter.Add(-time.Hour)) {
		return cached, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	number, err := serial()
	if err != nil {
		return nil, err
	}
	expiry := time.Now().Add(7 * 24 * time.Hour)
	if expiry.After(a.cert.NotAfter) {
		expiry = a.cert.NotAfter
	}
	template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pair := &tls.Certificate{Certificate: [][]byte{der, a.cert.Raw}, PrivateKey: key, Leaf: leaf}
	a.leaves[host] = pair
	return pair, nil
}

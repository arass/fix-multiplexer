package fixadapter

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"time"

	"github.com/quickfixgo/quickfix"
)

// privateBridgeTLS creates an in-memory identity for this process only. The
// engine and bridge mutually authenticate before any upstream connection.
func privateBridgeTLS(settings *quickfix.Settings) (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.New("cannot create private transport identity")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, errors.New("cannot create private transport identity")
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, errors.New("cannot create private transport identity")
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, errors.New("cannot create private transport identity")
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})
	identity, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("cannot create private transport identity")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		return nil, errors.New("cannot create private transport identity")
	}
	g := settings.GlobalSettings()
	g.SetRaw("SocketCertificateBytes", certPEM)
	g.SetRaw("SocketPrivateKeyBytes", keyPEM)
	g.SetRaw("SocketCABytes", certPEM)
	g.Set("SocketServerName", "127.0.0.1")
	g.Set("SocketUseSSL", "Y")
	g.Set("SocketInsecureSkipVerify", "N")
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}, nil
}

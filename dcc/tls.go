package dcc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"math/big"
	"sync"
	"time"
)

// clientTLSConfig is used when we connect to a bot's SSEND offer. Bots use
// self-signed certificates and DCC has no way to pin one, so the peer is not
// verified: SSL here protects against passive eavesdropping (ISP, network),
// not against an active man in the middle.
var clientTLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // see above

var (
	serverTLSOnce sync.Once
	serverTLS     *tls.Config
)

// serverTLSConfig is used when a bot connects to us for a passive SSEND. The
// certificate is generated in memory per process; bots don't verify it either.
func serverTLSConfig() *tls.Config {
	serverTLSOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Printf("dcc: generating TLS key: %v", err)
			serverTLS = &tls.Config{}
			return
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: "xirc"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(10, 0, 0),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			log.Printf("dcc: creating TLS certificate: %v", err)
			serverTLS = &tls.Config{}
			return
		}
		serverTLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	})
	return serverTLS
}

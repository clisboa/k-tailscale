package helper

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"net/http"
)

// Use a reviewed CA snapshot for public update services: Kobo firmware may
// provide an absent or outdated system store. These are TLS CAs, not the
// independent Tailscale/addon package-signature trust anchors.
//
//go:embed ca/cacert.pem
var updateCABundle []byte

func updateTransport() (*http.Transport, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(updateCABundle) {
		return nil, errors.New("embedded update CA bundle is invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	return transport, nil
}

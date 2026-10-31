package helper

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Removing the explicit updater roots must break the public-chain checks even
// on development machines with working or previously cached system roots.
func emptySystemRoots(t *testing.T) bool {
	t.Helper()
	if os.Getenv("KTAILSCALE_EMPTY_CA_CHILD") != "1" {
		directory := t.TempDir()
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "KTAILSCALE_EMPTY_CA_CHILD=1",
			"SSL_CERT_FILE="+filepath.Join(directory, "absent.pem"), "SSL_CERT_DIR="+directory)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("empty-system-CA subprocess: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return false
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots.Subjects()) != 0 {
		t.Fatal("test requires an empty system CA store")
	}
	return true
}

func TestUpdaterTLSWithoutSystemRoots(t *testing.T) {
	if !emptySystemRoots(t) {
		return
	}
	upstream, err := NewUpstream()
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport)
	if upstream.client.Transport != nil {
		transport = upstream.client.Transport.(*http.Transport)
	}
	var roots *x509.CertPool
	if transport.TLSClientConfig != nil {
		roots = transport.TLSClientConfig.RootCAs
	}
	for _, host := range []string{"pkgs.tailscale.com", "github.com"} {
		t.Run(host, func(t *testing.T) {
			data, err := os.ReadFile("testdata/tls/" + host + ".pem")
			if err != nil {
				t.Fatal(err)
			}
			var chain []*x509.Certificate
			for len(data) > 0 {
				block, rest := pem.Decode(data)
				if block == nil {
					t.Fatal("invalid certificate fixture")
				}
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					t.Fatal(err)
				}
				chain = append(chain, cert)
				data = rest
			}
			intermediates := x509.NewCertPool()
			for _, cert := range chain[1:] {
				intermediates.AddCert(cert)
			}
			options := x509.VerifyOptions{Roots: roots, Intermediates: intermediates,
				DNSName: host, CurrentTime: chain[0].NotBefore.Add(time.Hour)}
			if _, err := chain[0].Verify(options); err != nil {
				t.Fatalf("public update server is not trusted: %v", err)
			}
			options.DNSName = "wrong.example.invalid"
			if _, err := chain[0].Verify(options); err == nil {
				t.Fatal("wrong hostname accepted")
			}
			options.DNSName = host
			options.CurrentTime = chain[0].NotAfter.Add(time.Hour)
			if _, err := chain[0].Verify(options); err == nil {
				t.Fatal("expired certificate accepted")
			}
		})
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted server reached") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	upstream.baseURL = server.URL
	_, err = upstream.Latest(context.Background())
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Fatalf("untrusted HTTPS server was not rejected for unknown CA: %v", err)
	}
}

// Opt-in release smoke check; ordinary test runs stay deterministic and offline.
func TestUpdaterPublicTLS(t *testing.T) {
	if os.Getenv("KTAILSCALE_TEST_PUBLIC_TLS") != "1" {
		t.Skip("enable public HTTPS release smoke check explicitly")
	}
	if !emptySystemRoots(t) {
		return
	}
	upstream, err := NewUpstream()
	if err != nil {
		t.Fatal(err)
	}
	version, err := upstream.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Tailscale stable metadata: %s", version)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"https://github.com/clisboa/k-tailscale/releases/latest/download/update.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := upstream.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected addon HTTP status: %s", response.Status)
	}
	t.Logf("Addon endpoint TLS verified: %s (%s)", response.Status, strings.TrimSpace(response.Request.URL.Host))
}

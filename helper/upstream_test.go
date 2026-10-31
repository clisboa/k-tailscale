package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"tailscale.com/clientupdate/distsign"
)

func signedUpstream(t *testing.T) (*Upstream, map[string][]byte) {
	t.Helper()
	rootPrivate, rootPublic, err := distsign.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	root, err := distsign.ParseRootKey(rootPrivate)
	if err != nil {
		t.Fatal(err)
	}
	private, public, err := distsign.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := distsign.ParseSigningKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keySignature, err := root.SignSigningKeys(public)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(fixtureArchive(t, validHeaders()))
	if err != nil {
		t.Fatal(err)
	}
	hash := distsign.NewPackageHash()
	_, _ = hash.Write(archive)
	signature, err := signer.SignPackageHash(hash.Sum(nil), hash.Len())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"/stable/":      []byte(`{"TarballsVersion":"1.104.4"}`),
		"/distsign.pub": public, "/distsign.pub.sig": keySignature,
		"/stable/tailscale_1.104.4_arm.tgz":     archive,
		"/stable/tailscale_1.104.4_arm.tgz.sig": signature,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	roots, err := distsign.ParseRootKeyBundle(rootPublic)
	if err != nil {
		t.Fatal(err)
	}
	return &Upstream{baseURL: server.URL, client: server.Client(), roots: roots, free: func(string) (int64, error) { return 1 << 30, nil }}, files
}

func TestSignedUpstreamDownload(t *testing.T) {
	upstream, _ := signedUpstream(t)
	version, err := upstream.Latest(context.Background())
	if err != nil || version != "1.104.4" {
		t.Fatalf("metadata: %q, %v", version, err)
	}
	artifact, err := upstream.Download(context.Background(), version, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(artifact.Path)
	if artifact.Expanded != 40 {
		t.Fatalf("incorrect expanded size: %+v", artifact)
	}
}

func TestOfflineInstallProof(t *testing.T) {
	upstream, files := signedUpstream(t)
	artifact, err := upstream.Download(context.Background(), "1.104.4", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upstream.VerifyLocal(artifact.Path, artifact.Version, artifact.Proof); err != nil {
		t.Fatal(err)
	}
	proof := artifact.Proof
	proof.PackageSignature = []byte("invalid")
	if _, err := upstream.VerifyLocal(artifact.Path, artifact.Version, proof); err == nil {
		t.Fatal("invalid offline proof accepted")
	}
	if err := os.WriteFile(artifact.Path, append(files["/stable/tailscale_1.104.4_arm.tgz"], 0), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := upstream.VerifyLocal(artifact.Path, artifact.Version, artifact.Proof); err == nil {
		t.Fatal("changed offline archive accepted")
	}
}

func TestSignedUpstreamRejectsTampering(t *testing.T) {
	for _, target := range []string{"/distsign.pub.sig", "/stable/tailscale_1.104.4_arm.tgz.sig", "/stable/tailscale_1.104.4_arm.tgz"} {
		t.Run(target, func(t *testing.T) {
			upstream, files := signedUpstream(t)
			files[target] = append([]byte("corrupt"), files[target]...)
			dir := t.TempDir()
			if _, err := upstream.Download(context.Background(), "1.104.4", dir); err == nil {
				t.Fatal("tampered release accepted")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("unverified download retained")
			}
		})
	}
}

func TestUpstreamRejectsMetadataAndSpace(t *testing.T) {
	upstream, files := signedUpstream(t)
	for _, value := range []string{`{"TarballsVersion":"1.105.0"}`, strings.Repeat("x", 70<<10)} {
		files["/stable/"] = []byte(value)
		if _, err := upstream.Latest(context.Background()); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	upstream.free = func(string) (int64, error) { return 1, nil }
	if _, err := upstream.Download(context.Background(), "1.104.4", t.TempDir()); err == nil {
		t.Fatal("download started without space")
	}
}

func TestUpstreamCancellation(t *testing.T) {
	upstream, _ := signedUpstream(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := upstream.Latest(ctx); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

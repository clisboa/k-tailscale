package helper

import (
	"context"
	"crypto/ed25519"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"tailscale.com/clientupdate/distsign"
)

const maxDownload = 64 << 20
const spaceReserve = 16 << 20

// These public trust anchors are copied unchanged from Tailscale v1.104.4.
//
//go:embed roots/*.pem
var upstreamRoots embed.FS

type Upstream struct {
	baseURL string
	client  *http.Client
	roots   []ed25519.PublicKey
	free    func(string) (int64, error)
}

type Artifact struct {
	Path     string            `json:"path"`
	Version  string            `json:"version"`
	Expanded int64             `json:"expanded"`
	Files    map[string]int64  `json:"files"`
	Proof    DistributionProof `json:"proof"`
}

type DistributionProof struct {
	Keys             []byte `json:"keys"`
	KeysSignature    []byte `json:"keys_signature"`
	PackageSignature []byte `json:"package_signature"`
}

func (u *Upstream) VerifyLocal(filename, version string, proof DistributionProof) (Artifact, error) {
	if !stableVersion(version) {
		return Artifact{}, errors.New("invalid stable release version")
	}
	f, err := os.Open(filename)
	if err != nil {
		return Artifact{}, err
	}
	defer f.Close()
	hash := distsign.NewPackageHash()
	n, err := io.Copy(hash, io.LimitReader(f, maxDownload+1))
	if err != nil {
		return Artifact{}, err
	}
	if n <= 0 || n > maxDownload {
		return Artifact{}, errors.New("invalid local package size")
	}
	if err := u.verifyHash(proof, hash.Sum(nil), n); err != nil {
		return Artifact{}, err
	}
	plan, err := inspectArchive(filename, version)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: filename, Version: version, Expanded: plan.Expanded, Files: plan.Files, Proof: proof}, nil
}

func (u *Upstream) verifyHash(proof DistributionProof, hash []byte, size int64) error {
	if len(proof.Keys) > 1<<20 || !distsign.VerifyAny(u.roots, proof.Keys, proof.KeysSignature) {
		return errors.New("Tailscale signing-key signature was rejected")
	}
	keys, err := distsign.ParseSigningKeyBundle(proof.Keys)
	if err != nil {
		return err
	}
	message := binary.LittleEndian.AppendUint64(hash, uint64(size))
	if !distsign.VerifyAny(keys, message, proof.PackageSignature) {
		return errors.New("Tailscale package signature was rejected")
	}
	return nil
}

func NewUpstream() (*Upstream, error) {
	roots := []ed25519.PublicKey{}
	entries, err := upstreamRoots.ReadDir("roots")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := upstreamRoots.ReadFile("roots/" + entry.Name())
		if err != nil {
			return nil, err
		}
		keys, err := distsign.ParseRootKeyBundle(data)
		if err != nil {
			return nil, err
		}
		roots = append(roots, keys...)
	}
	transport, err := updateTransport()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" || len(via) > 4 {
			return errors.New("unsafe package redirect")
		}
		return nil
	}}
	return &Upstream{baseURL: "https://pkgs.tailscale.com", client: client, roots: roots, free: freeBytes}, nil
}

func freeBytes(directory string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err != nil {
		return 0, err
	}
	block := uint64(stat.Bsize)
	if block == 0 {
		return 0, errors.New("invalid filesystem block size")
	}
	if uint64(stat.Bavail) > math.MaxInt64/block {
		return math.MaxInt64, nil
	}
	return int64(uint64(stat.Bavail) * block), nil
}

func (u *Upstream) small(ctx context.Context, route string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.baseURL+route, nil)
	if err != nil {
		return nil, err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("package service returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("package response exceeds size limit")
	}
	return data, nil
}

func (u *Upstream) Latest(ctx context.Context) (string, error) {
	data, err := u.small(ctx, "/stable/?mode=json", 64<<10)
	if err != nil {
		return "", err
	}
	var metadata struct{ TarballsVersion string }
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("invalid release metadata: %w", err)
	}
	if !stableVersion(metadata.TarballsVersion) {
		return "", errors.New("package service did not supply a stable release")
	}
	return metadata.TarballsVersion, nil
}

func (u *Upstream) signingKeys(ctx context.Context) (DistributionProof, error) {
	bundle, err := u.small(ctx, "/distsign.pub", 1<<20)
	if err != nil {
		return DistributionProof{}, err
	}
	signature, err := u.small(ctx, "/distsign.pub.sig", ed25519.SignatureSize)
	if err != nil {
		return DistributionProof{}, err
	}
	if !distsign.VerifyAny(u.roots, bundle, signature) {
		return DistributionProof{}, errors.New("Tailscale signing-key signature was rejected")
	}
	return DistributionProof{Keys: bundle, KeysSignature: signature}, nil
}

func (u *Upstream) VerifyCurrentLocal(ctx context.Context, filename, version string) (Artifact, error) {
	if !stableVersion(version) {
		return Artifact{}, errors.New("invalid stable release version")
	}
	proof, err := u.signingKeys(ctx)
	if err != nil {
		return Artifact{}, err
	}
	proof.PackageSignature, err = u.small(ctx, "/stable/tailscale_"+version+"_arm.tgz.sig", ed25519.SignatureSize)
	if err != nil {
		return Artifact{}, err
	}
	return u.VerifyLocal(filename, version, proof)
}

func (u *Upstream) downloadSize(ctx context.Context, route string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, u.baseURL+route, nil)
	if err != nil {
		return 0, err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("package HEAD returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength <= 0 || response.ContentLength > maxDownload {
		return 0, errors.New("package has an invalid download size")
	}
	return response.ContentLength, nil
}

func (u *Upstream) Download(ctx context.Context, version, directory string) (Artifact, error) {
	if !stableVersion(version) {
		return Artifact{}, errors.New("invalid stable release version")
	}
	proof, err := u.signingKeys(ctx)
	if err != nil {
		return Artifact{}, err
	}
	route := "/stable/tailscale_" + version + "_arm.tgz"
	size, err := u.downloadSize(ctx, route)
	if err != nil {
		return Artifact{}, err
	}
	available, err := u.free(directory)
	if err != nil {
		return Artifact{}, err
	}
	if available < size+spaceReserve {
		return Artifact{}, errors.New("not enough system space for the verified download")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.baseURL+route, nil)
	if err != nil {
		return Artifact{}, err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return Artifact{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Artifact{}, fmt.Errorf("package GET returned HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp(directory, ".upstream-download-*")
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	defer os.Remove(file.Name())
	hash := distsign.NewPackageHash()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, size+1))
	if err != nil {
		return Artifact{}, err
	}
	if n != size {
		return Artifact{}, errors.New("downloaded package size does not match")
	}
	if err := file.Sync(); err != nil {
		return Artifact{}, err
	}
	if err := file.Close(); err != nil {
		return Artifact{}, err
	}
	signature, err := u.small(ctx, route+".sig", ed25519.SignatureSize)
	if err != nil {
		return Artifact{}, err
	}
	proof.PackageSignature = signature
	if err := u.verifyHash(proof, hash.Sum(nil), n); err != nil {
		return Artifact{}, err
	}
	plan, err := inspectArchive(file.Name(), version)
	if err != nil {
		return Artifact{}, err
	}
	destination := filepath.Join(directory, "verified-"+version+".tgz")
	if err := os.Rename(file.Name(), destination); err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: destination, Version: version, Expanded: plan.Expanded, Files: plan.Files, Proof: proof}, nil
}

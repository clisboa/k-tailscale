package helper

import (
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type osService struct {
	paths   Paths
	machine elf.Machine
	class   elf.Class
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	room := (16 << 10) - len(b.data)
	if room > 0 {
		b.data = append(b.data, data[:min(room, len(data))]...)
	}
	return n, nil
}

func commandOutput(ctx context.Context, filename string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filename, args...)
	command.Dir = "/"
	var output boundedOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.data, fmt.Errorf("%s failed: %w", filepath.Base(filename), err)
	}
	return output.data, nil
}

func (s *osService) Version(ctx context.Context, filename string) (string, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return "", errors.New("unsafe executable file")
	}
	f, err := elf.Open(filename)
	if err != nil {
		return "", errors.New("release executable is not an ELF binary")
	}
	machine, class := s.machine, s.class
	if machine == elf.EM_NONE {
		machine = elf.EM_ARM
		class = elf.ELFCLASS32
	}
	valid := f.Machine == machine && f.Class == class && f.Data == elf.ELFDATA2LSB && (f.Type == elf.ET_EXEC || f.Type == elf.ET_DYN)
	f.Close()
	if !valid {
		return "", errors.New("release executable has the wrong architecture")
	}
	args := []string{"--version"}
	cli := filepath.Base(filename) == "tailscale"
	if cli {
		args = []string{"version", "--json"}
	}
	data, err := commandOutput(ctx, filename, args...)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if cli {
		var value struct {
			Short string `json:"short"`
		}
		if err := json.Unmarshal(data, &value); err != nil {
			return "", errors.New("release CLI version response is invalid")
		}
		version = value.Short
	} else {
		fields := strings.Fields(version)
		if len(fields) == 0 {
			return "", errors.New("empty daemon version")
		}
		version = strings.SplitN(fields[0], "-", 2)[0]
	}
	if !stableVersion(version) {
		return "", errors.New("release version is not a stable Tailscale version")
	}
	return version, nil
}

func (s *osService) pids(ctx context.Context) ([]int, error) {
	data, err := commandOutput(ctx, "pidof", "tailscaled")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return []int{}, nil
		}
		return nil, err
	}
	pids := []int{}
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 || pid == os.Getpid() {
			return nil, errors.New("unsafe daemon PID")
		}
		executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSuffix(executable, " (deleted)") != filepath.Join(s.paths.Runtime, "bin/tailscaled") {
			return nil, errors.New("another Tailscale daemon uses an unrecognized executable")
		}
		pids = append(pids, pid)
	}
	return pids, nil
}
func (s *osService) Running(ctx context.Context) (bool, error) {
	pids, err := s.pids(ctx)
	return len(pids) > 0, err
}

func (s *osService) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		pids, err := s.pids(ctx)
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			return nil
		}
		for _, pid := range pids {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("daemon did not stop; existing executables were retained")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *osService) Start(ctx context.Context) error {
	if running, err := s.Running(ctx); err != nil {
		return err
	} else if running {
		return nil
	}
	if err := ensureDirectory(filepath.Dir(s.paths.Socket), 0700); err != nil {
		return err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	command := exec.Command(filepath.Join(s.paths.Runtime, "bin/tailscaled"), "--statedir="+filepath.Join(s.paths.Runtime, "state"), "--socket="+s.paths.Socket)
	command.Dir = "/"
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (s *osService) healthOnce(ctx context.Context, version string) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.paths.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-tailscaled.sock/localapi/v0/status?peers=false", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("daemon LocalAPI is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("daemon status exceeds limit")
	}
	var status struct{ Version, BackendState string }
	if err := json.Unmarshal(data, &status); err != nil {
		return err
	}
	if strings.SplitN(status.Version, "-", 2)[0] != version || status.BackendState == "" {
		return errors.New("daemon reports an unexpected version or empty state")
	}
	return nil
}
func (s *osService) Healthy(ctx context.Context, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		if err := s.healthOnce(ctx, version); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("updated daemon did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (s *osService) DisableNativeUpdates(ctx context.Context) error {
	_, err := commandOutput(ctx, filepath.Join(s.paths.Runtime, "bin/tailscale"), "--socket="+s.paths.Socket, "set", "--auto-update=false")
	return err
}

func (s *osService) Logout(ctx context.Context) error {
	_, err := commandOutput(ctx, filepath.Join(s.paths.Runtime, "bin/tailscale"), "--socket="+s.paths.Socket, "logout")
	return err
}

func DefaultEngine() (*Engine, error) {
	u, err := NewUpstream()
	if err != nil {
		return nil, err
	}
	p := Paths{Runtime: "/usr/local/tailscale-runtime", Payload: "/mnt/onboard/.adds/k-tailscale/install", Helpers: "/usr/local/tailscale-ui", Base: "/usr/local/tailscale", Rules: "/etc/udev/rules.d/99-k-tailscale.rules", Lock: "/tmp/kobo-tailscale-connect.lock", Version: "/mnt/onboard/.kobo/version", ServiceLock: "/tmp/k-tailscale-service.lock", Links: "/usr/bin", Socket: "/var/run/tailscale/tailscaled.sock"}
	p.SystemRoot = "/"
	p.CoreLink = "/usr/local/k-tailscale/helper"
	p.CandidateHelper, err = os.Executable()
	if err != nil {
		return nil, err
	}
	addon, err := newAddonSource(u)
	if err != nil {
		return nil, err
	}
	return &Engine{paths: p, upstream: u, service: &osService{paths: p}, free: freeBytes, eligibility: deviceInstallAllowed, addon: addon}, nil
}
func (e *Engine) StartService(ctx context.Context) error {
	unlock, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		return errors.New("system setup is incomplete")
	}
	if err := e.recoverAddon(); err != nil {
		return err
	}
	if err := e.recoverUpdate(ctx, true); err != nil {
		return err
	}
	if err := e.validateRuntimeDirectories(); err != nil {
		return err
	}
	version, err := e.service.Version(ctx, filepath.Join(e.paths.Runtime, "bin/tailscale"))
	if err != nil {
		return err
	}
	if daemon, err := e.service.Version(ctx, filepath.Join(e.paths.Runtime, "bin/tailscaled")); err != nil {
		return err
	} else if daemon != version {
		return errors.New("executable pair mismatch")
	}
	if err := e.service.Start(ctx); err != nil {
		return err
	}
	if err := e.service.Healthy(ctx, version); err != nil {
		return err
	}
	return e.service.DisableNativeUpdates(ctx)
}

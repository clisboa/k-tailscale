package helper

import (
	"bytes"
	"context"
	"debug/elf"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

//go:embed kernels/clara-hd.json
var bundledTUNManifest []byte

type tunModuleSpec struct {
	Schema       int    `json:"schema"`
	Model        string `json:"model"`
	Kernel       string `json:"kernel"`
	VersionMagic string `json:"vermagic"`
	ModuleLayout string `json:"module_layout"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	InstallPath  string `json:"install_path"`
}

func bundledTUN() (tunModuleSpec, error) {
	var spec tunModuleSpec
	if err := json.Unmarshal(bundledTUNManifest, &spec); err != nil {
		return spec, err
	}
	return spec, validateTUNSpec(spec)
}

func validateTUNSpec(spec tunModuleSpec) error {
	hash, err := hex.DecodeString(spec.SHA256)
	_, crcErr := strconv.ParseUint(spec.ModuleLayout, 0, 32)
	if spec.Schema != 1 || spec.Model != "376" || spec.Kernel == "" || !strings.HasPrefix(spec.VersionMagic, spec.Kernel+" ") || spec.Size <= 0 || spec.Size > 2<<20 || err != nil || len(hash) != 32 || crcErr != nil || spec.InstallPath != "mnt/onboard/.adds/k-tailscale/kernel/clara-hd/tun.ko" {
		return errors.New("invalid bundled TUN module manifest")
	}
	return nil
}

func validateTUNFile(path string, spec tunModuleSpec) error {
	if err := validateTUNSpec(spec); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || owner.Uid != uint32(os.Geteuid()) || info.Size() != spec.Size {
		return errors.New("unsafe or changed TUN module file")
	}
	data, err := readRegular(path, 2<<20)
	if err != nil {
		return err
	}
	if int64(len(data)) != spec.Size || digest(data) != spec.SHA256 {
		return errors.New("TUN module SHA256 mismatch")
	}
	module, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer module.Close()
	if module.Class != elf.ELFCLASS32 || module.Data != elf.ELFDATA2LSB || module.Machine != elf.EM_ARM || module.Type != elf.ET_REL {
		return errors.New("TUN module must be a relocatable 32-bit ARM ELF")
	}
	metadata, versions := module.Section(".modinfo"), module.Section("__versions")
	if metadata == nil || versions == nil || metadata.Size > 64<<10 || versions.Size > 64<<10 {
		return errors.New("TUN module metadata is missing or oversized")
	}
	infoBytes, err := metadata.Data()
	if err != nil {
		return err
	}
	fields := map[string]string{}
	for _, entry := range bytes.Split(infoBytes, []byte{0}) {
		key, value, ok := strings.Cut(string(entry), "=")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if fields["vermagic"] != spec.VersionMagic || fields["license"] != "GPL" || fields["depends"] != "" {
		return errors.New("TUN module version, license or dependencies do not match")
	}
	versionBytes, err := versions.Data()
	if err != nil {
		return err
	}
	if len(versionBytes)%64 != 0 {
		return errors.New("invalid TUN module symbol versions")
	}
	crc, _ := strconv.ParseUint(spec.ModuleLayout, 0, 32)
	for offset := 0; offset < len(versionBytes); offset += 64 {
		name := strings.TrimRight(string(versionBytes[offset+4:offset+64]), "\x00")
		if name == "module_layout" && binary.LittleEndian.Uint32(versionBytes[offset:]) == uint32(crc) {
			return nil
		}
	}
	return errors.New("TUN module layout checksum does not match")
}

type tunHooks struct {
	verify func(string, tunModuleSpec) error
	probe  func() error
	kernel func() (string, error)
	load   func(context.Context, string) error
}

func ensureTUN(ctx context.Context, path string, spec tunModuleSpec, hooks tunHooks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := hooks.probe(); err == nil {
		return nil
	} else if !errors.Is(err, syscall.ENODEV) && !errors.Is(err, syscall.ENXIO) {
		return fmt.Errorf("cannot access TUN device: %w", err)
	}
	kernel, err := hooks.kernel()
	if err != nil {
		return err
	}
	if strings.TrimSpace(kernel) != spec.Kernel {
		return fmt.Errorf("this kernel (%s) has no usable TUN driver and no matching module is bundled", strings.TrimSpace(kernel))
	}
	verify := hooks.verify
	if verify == nil {
		verify = validateTUNFile
	}
	if err := verify(path, spec); err != nil {
		return fmt.Errorf("TUN driver is unavailable; reinstall the complete USB package: %w", err)
	}
	loadErr := hooks.load(ctx, path)
	// Another starter may have registered the exact driver first. Accept an
	// already-loaded result only when the device probe actually succeeds.
	if err := hooks.probe(); err == nil {
		return nil
	} else {
		return errors.Join(loadErr, fmt.Errorf("TUN driver did not become usable: %w", err))
	}
}

func probeTUNDevice(device string) error {
	info, err := os.Lstat(device)
	if os.IsNotExist(err) {
		if err := ensureDirectory(filepath.Dir(device), 0755); err != nil {
			return err
		}
		if err := syscall.Mknod(device, syscall.S_IFCHR|0600, 10<<8|200); err != nil && !errors.Is(err, syscall.EEXIST) {
			return err
		}
		info, err = os.Lstat(device)
	}
	if err != nil {
		return err
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeCharDevice == 0 || uint64(metadata.Rdev) != 10<<8|200 {
		return errors.New("existing TUN path is not the expected character device")
	}
	fd, err := syscall.Open(device, syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	return syscall.Close(fd)
}

func (s *osService) prepareTUN(ctx context.Context) error {
	if s.paths.TUNDevice == "" {
		return nil
	}
	spec, err := bundledTUN()
	if err != nil {
		return err
	}
	return ensureTUN(ctx, filepath.Join(s.paths.RAM, "kernel/tun.ko"), spec, tunHooks{
		probe:  func() error { return probeTUNDevice(s.paths.TUNDevice) },
		kernel: s.runningKernel,
		verify: func(path string, spec tunModuleSpec) error {
			engine := &Engine{paths: s.paths, service: s, free: freeBytes}
			return engine.ensureRAMTUN(ctx, path, spec)
		},
		load: func(ctx context.Context, path string) error {
			output, err := commandOutput(ctx, "/sbin/insmod", path)
			if err != nil {
				return fmt.Errorf("normal TUN module load failed: %s: %w", strings.TrimSpace(string(output)), err)
			}
			return nil
		},
	})
}

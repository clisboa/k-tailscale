package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Deliberately contains no profile, node key, login URL, serial or state bytes.
type storageReport struct {
	WiFiAvailable      bool     `json:"wifi_available"`
	WirelessInterfaces []string `json:"wireless_interfaces"`
	TUNRegistered      bool     `json:"tun_registered"`
	Schema             int      `json:"schema"`
	Version            string   `json:"version"`
	SystemFree         int64    `json:"system_free_bytes"`
	BooksFree          int64    `json:"books_free_bytes"`
	RAMFree            int64    `json:"ram_free_bytes"`
	MemoryAvailable    int64    `json:"memory_available_bytes"`
	ExecutableBytes    int64    `json:"executable_bytes"`
	RAMReady           bool     `json:"verified_ram_ready"`
	Running            bool     `json:"daemon_running"`
	Executables        []string `json:"daemon_executables"`
	BooksReferences    int      `json:"daemon_books_references"`
}

func daemonBooksReferences(pid int, mount string) (string, int, error) {
	base := "/proc/" + strconv.Itoa(pid)
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return "", 0, err
	}
	references := 0
	paths := []string{filepath.Join(base, "exe"), filepath.Join(base, "cwd")}
	files, err := filepath.Glob(filepath.Join(base, "fd/*"))
	if err != nil {
		return "", 0, err
	}
	paths = append(paths, files...)
	for _, path := range paths {
		target, err := os.Readlink(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", 0, err
		}
		if strings.HasPrefix(target, mount+"/") || target == mount {
			references++
		}
	}
	maps, err := os.ReadFile(filepath.Join(base, "maps"))
	if err != nil {
		return "", 0, err
	}
	for _, line := range strings.Split(string(maps), "\n") {
		if strings.Contains(line, " "+mount+"/") {
			references++
		}
	}
	return executable, references, nil
}

// StorageReport is a manually invoked support diagnostic. All public reads and
// the final closed report use the same USB guard as setup and binary updates.
func (e *Engine) StorageReport(ctx context.Context) error {
	lock, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer lock()
	return e.withPublicIO(ctx, func(context.Context) error {
		layout, err := e.readLayout()
		if err != nil {
			return err
		}
		report := storageReport{Schema: 1, Version: layout.Current.Version, RAMReady: e.ramPairReady(layout.Current), Executables: []string{}}
		report.WiFiAvailable = wifiPresent()
		wireless, _ := os.ReadFile("/proc/net/wireless")
		report.WirelessInterfaces = []string{}
		for name := range wirelessInterfaces(wireless) {
			report.WirelessInterfaces = append(report.WirelessInterfaces, name)
		}
		if info, statErr := os.Stat("/sys/class/misc/tun"); statErr == nil {
			report.TUNRegistered = info.IsDir()
		}
		if report.ExecutableBytes, err = validatePin(layout.Current); err != nil {
			return err
		}
		if report.SystemFree, err = e.free(e.paths.Runtime); err != nil {
			return err
		}
		if report.BooksFree, err = e.free(e.paths.Books); err != nil {
			return err
		}
		if report.RAMFree, err = e.free(e.paths.RAM); err != nil {
			return err
		}
		memory := e.memory
		if memory == nil {
			memory = availableMemory
		}
		if report.MemoryAvailable, err = memory(); err != nil {
			return err
		}
		if report.Running, err = e.service.Running(ctx); err != nil {
			return err
		}
		if service, ok := e.service.(*osService); ok {
			pids, err := service.pids(ctx)
			if err != nil {
				return err
			}
			for _, pid := range pids {
				executable, references, err := daemonBooksReferences(pid, e.paths.BooksMount)
				if err != nil {
					return err
				}
				report.Executables = append(report.Executables, executable)
				report.BooksReferences += references
			}
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		directory := e.paths.Books
		if e.paths.BooksMount != "" {
			directory = filepath.Join(e.paths.BooksMount, ".adds/tailscale-ui")
		}
		if err := e.ensurePublicDirectory(directory); err != nil {
			return err
		}
		return writeAtomicPublic(filepath.Join(directory, "storage-report.json"), append(data, '\n'))
	})
}

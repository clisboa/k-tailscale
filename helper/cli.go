package helper

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// BuildVersion is supplied by the release build; development builds do not self-update.
var BuildVersion = "dev"

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprintln(stderr, "Usage: k-tailscale-helper version | fetch-release --directory PATH [--version X.Y.Z]")
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	if args[0] == "version" && len(args) == 1 {
		if err := json.NewEncoder(stdout).Encode(map[string]string{"version": BuildVersion}); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "verify-tun-module" {
		flags := flag.NewFlagSet("verify-tun-module", flag.ContinueOnError)
		flags.SetOutput(stderr)
		module := flags.String("module", "", "built TUN module")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		if flags.NArg() != 0 || *module == "" {
			return usage()
		}
		spec, err := bundledTUN()
		if err == nil {
			err = validateTUNFile(*module, spec)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if json.NewEncoder(stdout).Encode(spec) != nil {
			return 1
		}
		return 0
	}
	if args[0] == "verify-payload" {
		flags := flag.NewFlagSet("verify-payload", flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifest := flags.String("manifest", "", "public release proof JSON")
		archive := flags.String("archive", "", "official release archive")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 || *manifest == "" || *archive == "" {
			return usage()
		}
		var claimed Artifact
		err := readJSON(*manifest, &claimed)
		var verified Artifact
		if err == nil {
			var upstream *Upstream
			upstream, err = NewUpstream()
			if err == nil {
				verified, err = upstream.VerifyLocal(*archive, claimed.Version, claimed.Proof)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(verified); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && koboCommand(args[0]) {
		if runtime.GOOS != "linux" || os.Geteuid() != 0 {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": "This command runs only on the Kobo as root."})
			return 1
		}
		syscall.Umask(0077)
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		if args[0] == "wifi-ready" {
			if wifiPresent() {
				return 0
			}
			fmt.Fprintln(stderr, "Connect Wi-Fi using the Kobo's Wi-Fi control, then retry.")
			return 1
		}
		engine, err := DefaultEngine()
		if err == nil {
			switch args[0] {
			case "storage-report":
				err = engine.StorageReport(ctx)
			case "usb-begin":
				err = engine.BeginUSB(ctx)
			case "usb-end":
				err = engine.EndUSB()
			case "logout":
				err = engine.SignOut(ctx)
			case "progress":
				var progress map[string]string
				progress, err = engine.Progress()
				if err == nil {
					if json.NewEncoder(stdout).Encode(progress) != nil {
						return 1
					}
					return 0
				}
			case "checks-on", "checks-off":
				err = engine.SetChecks(args[0] == "checks-on")
			case "recover-files":
				err = engine.RecoverFiles(ctx)
			case "prepare":
				err = engine.Prepare(ctx)
				engine.reportSetup(ctx, err)
			case "service-start", "recover":
				err = engine.StartService(ctx)
			case "install-addon":
				err = engine.InstallAddon(ctx)
			case "install":
				err = engine.InstallTailscale(ctx)
			case "check", "check-auto":
				err = engine.Check(ctx, args[0] == "check-auto")
			case "status":
				var status UpdateStatus
				status, err = engine.Status(ctx)
				if err == nil {
					if json.NewEncoder(stdout).Encode(status) != nil {
						return 1
					}
					return 0
				}
			}
		}
		if err != nil {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
			return 1
		}
		_ = json.NewEncoder(stdout).Encode(map[string]string{"message": "Action completed."})
		return 0
	}
	if args[0] != "fetch-release" {
		return usage()
	}
	flags := flag.NewFlagSet("fetch-release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "official stable version (default: latest)")
	directory := flags.String("directory", "", "private artifact output directory")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || (*version != "" && !stableVersion(*version)) {
		return usage()
	}
	upstream, err := NewUpstream()
	if err == nil && *version == "" {
		*version, err = upstream.Latest(ctx)
	}
	if err == nil {
		err = os.MkdirAll(*directory, 0700)
	}
	var artifact Artifact
	if err == nil {
		cached := filepath.Join(*directory, "verified-"+*version+".tgz")
		if _, statErr := os.Lstat(cached); statErr == nil {
			artifact, err = upstream.VerifyCurrentLocal(ctx, cached, *version)
		} else if os.IsNotExist(statErr) {
			artifact, err = upstream.Download(ctx, *version, *directory)
		} else {
			err = statErr
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(artifact); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func koboCommand(name string) bool {
	switch name {
	case "logout", "prepare", "service-start", "status", "check", "check-auto", "install", "recover", "install-addon", "recover-files", "checks-on", "checks-off", "progress", "usb-begin", "usb-end", "storage-report", "wifi-ready":
		return true
	}
	return false
}

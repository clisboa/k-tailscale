package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedDevicePairs(t *testing.T) {
	cases := []struct {
		name, version string
		accepted      bool
	}{
		{name: "Libra", version: "a,b,4.46.23852,d,e,00000000-0000-0000-0000-000000000390", accepted: true},
		{name: "Clara", version: "a,b,4.38.23852,d,e,00000000-0000-0000-0000-000000000376", accepted: true},
		{name: "Clara with Libra firmware", version: "a,b,4.46.23852,d,e,376"},
		{name: "Libra with Clara firmware", version: "a,b,4.38.23852,d,e,390"},
		{name: "unknown Clara firmware", version: "a,b,4.99.0,d,e,376"},
		{name: "unknown model", version: "a,b,4.38.23852,d,e,393"},
		{name: "missing fields", version: "a,b,4.38.23852"},
		{name: "empty model", version: "a,b,4.38.23852,d,e,"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "version")
			if err := os.WriteFile(path, []byte(tc.version), 0600); err != nil {
				t.Fatal(err)
			}
			e := &Engine{paths: Paths{Version: path}}
			err := e.supportedDevice()
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
			}
		})
	}
}

func TestPrepareInstallsModelHostnameWithoutBookStorageDependency(t *testing.T) {
	for _, tc := range []struct{ model, firmware, hostname string }{
		{model: "390", firmware: "4.46.23852", hostname: "kobo-libra-colour"},
		{model: "376", firmware: "4.38.23852", hostname: "kobo-clara-hd"},
	} {
		t.Run(tc.hostname, func(t *testing.T) {
			e, _ := setupFixture(t)
			version := "a,b," + tc.firmware + ",d,e," + tc.model
			if err := os.WriteFile(e.paths.Version, []byte(version), 0600); err != nil {
				t.Fatal(err)
			}
			if err := e.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(e.paths.Base, "connect.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "--hostname="+tc.hostname) {
				t.Fatalf("wrong generated hostname: %s", data)
			}
			if got := strings.Contains(string(data), "--netfilter-mode=off"); got != (tc.model == "376") {
				t.Fatalf("wrong model-specific firewall setting: %s", data)
			}
			if strings.Contains(string(data), "/mnt/onboard") {
				t.Fatal("connection script depends on books storage")
			}
			if err := os.Remove(e.paths.Version); err != nil {
				t.Fatal(err)
			}
			if err := e.StartService(context.Background()); err != nil {
				t.Fatalf("runtime startup needs the books partition: %v", err)
			}
		})
	}
}

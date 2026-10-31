package helper

import "testing"

func TestStableVersion(t *testing.T) {
	for _, version := range []string{"1.104.4", "1.106.0", "2.0.1"} {
		if !stableVersion(version) {
			t.Errorf("stable release rejected: %q", version)
		}
	}
	for _, version := range []string{"", "v1.104.4", "1.105.0", "1.104.4-rc1", "01.104.4", "1.104.4/../../x", "1.104.4\n", "999999999999999999.2.0"} {
		if stableVersion(version) {
			t.Errorf("invalid/unstable version accepted: %q", version)
		}
	}
}

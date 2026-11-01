package helper

import "testing"

func TestWiFiRoutesRecognizeLegacyWirelessNamesWithoutCountingEthernetOrVPN(t *testing.T) {
	wireless := []byte("Inter-| sta-| Quality | Discarded packets\n face | tus | link level noise\n eth0: 0000 70. -40. -256 0 0 0 0 0 0\n")
	for _, tc := range []struct {
		route string
		radio []byte
		want  bool
	}{
		{"eth0 00000000 0100A8C0 0003\n", wireless, true},
		{"eth0 00000000 0100A8C0 0003\n", nil, false},
		{"wlan0 00000000 0100A8C0 0003\n", nil, true},
		{"tailscale0 00000000 0100A8C0 0003\n", wireless, false},
		{"usb0 00000000 0100A8C0 0003\n", wireless, false},
		{"eth0 00000000 0100A8C0 0000\n", wireless, false},
		{"eth0 00000000 00000000 0001\n", wireless, false},
		{"eth0 00000000 0100A8C0 0001\n", wireless, false},
		{"eth0 00A8FE00 00000000 0001\n", wireless, false},
	} {
		if got := wifiRoutesAvailable([]byte(tc.route), tc.radio); got != tc.want {
			t.Errorf("route %q, wireless=%q: %v", tc.route, tc.radio, got)
		}
	}
}

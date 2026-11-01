package helper

import (
	"os"
	"strconv"
	"strings"
)

func wirelessInterfaces(data []byte) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasSuffix(fields[0], ":") {
			names[strings.TrimSuffix(fields[0], ":")] = true
		}
	}
	return names
}

func wifiRoutesAvailable(routes, wireless []byte) bool {
	names := wirelessInterfaces(wireless)
	for _, line := range strings.Split(string(routes), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || (!strings.HasPrefix(fields[0], "wlan") && !names[fields[0]]) || fields[1] != "00000000" || fields[2] == "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err == nil && flags&3 == 3 {
			return true
		}
	}
	return false
}

func wifiPresent() bool {
	routes, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return false
	}
	wireless, _ := os.ReadFile("/proc/net/wireless")
	return wifiRoutesAvailable(routes, wireless)
}

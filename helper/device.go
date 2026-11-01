package helper

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type deviceProfile struct {
	name, firmware, hostname string
	linksMode                os.FileMode
	wifiInterfaces           []string
	netfilterMode            string
}

var supportedDevices = map[string]deviceProfile{
	"390": {name: "Kobo Libra Colour", firmware: "4.46.23852", hostname: "kobo-libra-colour", linksMode: 0755, wifiInterfaces: []string{"wlan*"}},
	"376": {name: "Kobo Clara HD", firmware: "4.38.23852", hostname: "kobo-clara-hd", linksMode: 0775, wifiInterfaces: []string{"wlan*", "eth0"}, netfilterMode: "off"},
}

func (e *Engine) deviceProfile() (deviceProfile, error) {
	file, err := os.Open(e.paths.Version)
	if err != nil {
		return deviceProfile{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return deviceProfile{}, err
	}
	fields := strings.Split(strings.TrimSpace(string(data)), ",")
	if len(data) > 4096 || len(fields) < 6 {
		return deviceProfile{}, errors.New("invalid Kobo version file")
	}
	parts := strings.Split(fields[5], "-")
	model := strings.TrimLeft(parts[len(parts)-1], "0")
	profile, ok := supportedDevices[model]
	if !ok {
		return deviceProfile{}, errors.New("this installer supports Kobo Libra Colour and Kobo Clara HD")
	}
	if fields[2] != profile.firmware {
		return deviceProfile{}, fmt.Errorf("%s requires firmware %s", profile.name, profile.firmware)
	}
	return profile, nil
}

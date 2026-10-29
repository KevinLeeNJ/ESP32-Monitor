//go:build windows

package collector

import (
	"errors"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func windowsSystemTags() map[string]string {
	values := map[string]string{}
	read := func(path string, names []string) {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer key.Close()
		for _, name := range names {
			if value, _, err := key.GetStringValue(name); err == nil {
				values[name] = value
				continue
			}
			if value, _, err := key.GetIntegerValue(name); err == nil {
				values[name] = strconv.FormatUint(value, 10)
			}
		}
	}
	read(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, []string{"ProductName", "DisplayVersion", "ReleaseId", "CurrentBuild", "UBR", "EditionID"})
	read(`HARDWARE\DESCRIPTION\System\BIOS`, []string{"SystemManufacturer", "SystemProductName", "BaseBoardManufacturer", "BaseBoardProduct"})
	return windowsSystemInfo(values)
}

func windowsSystemInfo(values map[string]string) map[string]string {
	tags := map[string]string{}
	build := strings.TrimSpace(values["CurrentBuild"])
	if ubr := strings.TrimSpace(values["UBR"]); ubr != "" && build != "" {
		build += "." + ubr
	}
	if name := strings.TrimSpace(values["ProductName"]); name != "" {
		tags["os_name"] = windowsProductName(name, build)
	}
	if version := strings.TrimSpace(values["DisplayVersion"]); version != "" {
		tags["os_version"] = version
	} else if release := strings.TrimSpace(values["ReleaseId"]); release != "" {
		tags["os_version"] = release
	}
	if build != "" {
		tags["os_build"] = build
	}
	if product := windowsProduct(values["SystemManufacturer"], values["SystemProductName"], values["BaseBoardProduct"]); product != "" {
		tags["product_name"] = product
	}
	return tags
}

func windowsBuildNumber(build string) int {
	major := build
	if index := strings.IndexByte(major, '.'); index >= 0 {
		major = major[:index]
	}
	value, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return value
}

// Windows 11 still reports ProductName "Windows 10" in the registry; the build
// number is the authoritative discriminator.
func windowsProductName(product, build string) string {
	if windowsBuildNumber(build) >= 22000 && strings.HasPrefix(product, "Windows 10") {
		return strings.Replace(product, "Windows 10", "Windows 11", 1)
	}
	return product
}

var windowsPlaceholderProducts = map[string]bool{
	"": true, "default string": true, "system product name": true, "to be filled by o.e.m.": true,
	"oem": true, "not specified": true, "unknown": true, "none": true,
}

func windowsProduct(manufacturer, model, board string) string {
	model = strings.TrimSpace(model)
	if windowsPlaceholderProducts[strings.ToLower(model)] {
		model = strings.TrimSpace(board)
	}
	if windowsPlaceholderProducts[strings.ToLower(model)] {
		return ""
	}
	manufacturer = strings.TrimSpace(manufacturer)
	if windowsPlaceholderProducts[strings.ToLower(manufacturer)] {
		manufacturer = ""
	}
	if manufacturer == "" || strings.Contains(strings.ToLower(model), strings.ToLower(manufacturer)) {
		return model
	}
	return manufacturer + " " + model
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

const (
	windowsBatteryCharging  = 8
	windowsBatteryNoSystem  = 128
	windowsBatteryUndefined = 255
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

func windowsPowerStatus() (systemPowerStatus, error) {
	var status systemPowerStatus
	result, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		if err == nil {
			err = errors.New("GetSystemPowerStatus failed")
		}
		return status, err
	}
	return status, nil
}

// Level and charging status are a pair; an unknown or absent battery yields no
// fields so a desktop never looks like an empty battery.
func batteryFields(status systemPowerStatus) (map[string]float64, bool) {
	if status.BatteryFlag == windowsBatteryUndefined || status.BatteryFlag&windowsBatteryNoSystem != 0 || status.BatteryLifePercent > 100 {
		return nil, false
	}
	charging := 0.0
	if status.BatteryFlag&windowsBatteryCharging != 0 {
		charging = 1
	}
	return map[string]float64{"charge_percent": float64(status.BatteryLifePercent), "charging": charging}, true
}

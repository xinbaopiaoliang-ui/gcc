//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type registryStringValue struct {
	value  string
	exists bool
}

type registryDWordValue struct {
	value  uint64
	exists bool
}

func enableWindowsSystemProxy(listen string) (func() error, error) {
	proxyAddress := strings.TrimPrefix(listen, "http://")
	proxyAddress = strings.TrimPrefix(proxyAddress, "https://")
	if proxyAddress == "" {
		return nil, errors.New("proxy listen address is required")
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()

	oldEnable := readDWordValue(key, "ProxyEnable")
	oldServer := readStringValue(key, "ProxyServer")
	oldOverride := readStringValue(key, "ProxyOverride")

	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return nil, err
	}
	if err := key.SetStringValue("ProxyServer", "http="+proxyAddress+";https="+proxyAddress); err != nil {
		return nil, err
	}
	if !oldOverride.exists {
		if err := key.SetStringValue("ProxyOverride", "<local>"); err != nil {
			return nil, err
		}
	}
	notifyProxyChanged()

	return func() error {
		restoreKey, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer restoreKey.Close()

		if err := restoreDWordValue(restoreKey, "ProxyEnable", oldEnable); err != nil {
			return err
		}
		if err := restoreStringValue(restoreKey, "ProxyServer", oldServer); err != nil {
			return err
		}
		if err := restoreStringValue(restoreKey, "ProxyOverride", oldOverride); err != nil {
			return err
		}
		notifyProxyChanged()
		return nil
	}, nil
}

func launchSteamClient(steamExe, steamURL string) error {
	steamURL = strings.TrimSpace(steamURL)
	if steamURL == "" {
		steamURL = "steam://open/store"
	}
	steamExe = strings.TrimSpace(steamExe)
	if steamExe == "" {
		steamExe = detectSteamExe()
	}
	if steamExe != "" {
		return exec.Command(steamExe, steamURL).Start()
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", steamURL).Start()
}

func detectSteamExe() string {
	candidates := []string{}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, path := range []string{`Software\Valve\Steam`, `Software\WOW6432Node\Valve\Steam`} {
			key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			if value, _, err := key.GetStringValue("SteamExe"); err == nil {
				candidates = append(candidates, value)
			}
			if value, _, err := key.GetStringValue("SteamPath"); err == nil {
				candidates = append(candidates, filepath.Join(value, "steam.exe"))
			}
			key.Close()
		}
	}
	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		candidates = append(candidates, filepath.Join(programFilesX86, "Steam", "steam.exe"))
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "Steam", "steam.exe"))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func readStringValue(key registry.Key, name string) registryStringValue {
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return registryStringValue{}
	}
	return registryStringValue{value: value, exists: true}
}

func readDWordValue(key registry.Key, name string) registryDWordValue {
	value, _, err := key.GetIntegerValue(name)
	if err != nil {
		return registryDWordValue{}
	}
	return registryDWordValue{value: value, exists: true}
}

func restoreStringValue(key registry.Key, name string, old registryStringValue) error {
	if !old.exists {
		err := key.DeleteValue(name)
		if err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return err
		}
		return nil
	}
	return key.SetStringValue(name, old.value)
}

func restoreDWordValue(key registry.Key, name string, old registryDWordValue) error {
	if !old.exists {
		err := key.DeleteValue(name)
		if err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return err
		}
		return nil
	}
	return key.SetDWordValue(name, uint32(old.value))
}

func notifyProxyChanged() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	const (
		internetOptionRefresh         = 37
		internetOptionSettingsChanged = 39
	)
	_, _, _ = internetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = internetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
}

func systemProxyModeDescription() string {
	return fmt.Sprintf("Windows current-user proxy registry path: HKCU\\%s", internetSettingsPath)
}

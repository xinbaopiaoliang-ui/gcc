//go:build !windows

package main

import "errors"

func enableWindowsSystemProxy(string) (func() error, error) {
	return nil, errors.New("temporary system proxy mode is only supported on Windows")
}

func launchSteamClient(string, string) error {
	return errors.New("launching Steam from this demo is only supported on Windows")
}

func systemProxyModeDescription() string {
	return "Windows system proxy mode is unavailable on this platform"
}

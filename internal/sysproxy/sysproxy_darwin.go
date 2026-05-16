//go:build darwin

package sysproxy

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var mu sync.Mutex
var saved *darwinState

type darwinState struct {
	Service string
	Web     proxyState
	Secure  proxyState
	Socks   proxyState
}

type proxyState struct {
	Enabled bool
	Server  string
	Port    string
}

// Apply 使用 networksetup 设置 HTTP/HTTPS 代理；若 socksHost、socksPort 非空则同时设置 SOCKS 代理。
func Apply(httpHost, httpPort, socksHost, socksPort, networkService string) error {
	mu.Lock()
	defer mu.Unlock()

	svc, err := resolveService(networkService)
	if err != nil {
		return err
	}
	web, err := getProxyState("web", svc)
	if err != nil {
		return err
	}
	sec, err := getProxyState("secureweb", svc)
	if err != nil {
		return err
	}
	socks, err := getProxyState("socks", svc)
	if err != nil {
		return err
	}
	saved = &darwinState{Service: svc, Web: web, Secure: sec, Socks: socks}

	if _, err := runNetworksetup("-setwebproxy", svc, httpHost, httpPort); err != nil {
		return fmt.Errorf("setwebproxy: %w", err)
	}
	if _, err := runNetworksetup("-setsecurewebproxy", svc, httpHost, httpPort); err != nil {
		_ = restoreDarwinLocked()
		return fmt.Errorf("setsecurewebproxy: %w", err)
	}
	if _, err := runNetworksetup("-setwebproxystate", svc, "on"); err != nil {
		_ = restoreDarwinLocked()
		return fmt.Errorf("setwebproxystate on: %w", err)
	}
	if _, err := runNetworksetup("-setsecurewebproxystate", svc, "on"); err != nil {
		_ = restoreDarwinLocked()
		return fmt.Errorf("setsecurewebproxystate on: %w", err)
	}

	if socksHost != "" && socksPort != "" {
		if _, err := runNetworksetup("-setsocksfirewallproxy", svc, socksHost, socksPort); err != nil {
			_ = restoreDarwinLocked()
			return fmt.Errorf("setsocksfirewallproxy: %w", err)
		}
		if _, err := runNetworksetup("-setsocksfirewallproxystate", svc, "on"); err != nil {
			_ = restoreDarwinLocked()
			return fmt.Errorf("setsocksfirewallproxystate on: %w", err)
		}
	}
	return nil
}

// Restore 恢复 Apply 之前保存的 HTTP/HTTPS/SOCKS 代理设置。
func Restore() error {
	mu.Lock()
	defer mu.Unlock()
	return restoreDarwinLocked()
}

func restoreDarwinLocked() error {
	if saved == nil {
		return nil
	}
	svc := saved.Service
	st := saved
	saved = nil

	if err := restoreOne(svc, "web", st.Web); err != nil {
		return err
	}
	if err := restoreOne(svc, "secureweb", st.Secure); err != nil {
		return err
	}
	if err := restoreOne(svc, "socks", st.Socks); err != nil {
		return err
	}
	return nil
}

func restoreOne(svc, kind string, st proxyState) error {
	switch kind {
	case "web":
		if !st.Enabled {
			_, err := runNetworksetup("-setwebproxystate", svc, "off")
			return err
		}
		if _, err := runNetworksetup("-setwebproxy", svc, st.Server, st.Port); err != nil {
			return err
		}
		_, err := runNetworksetup("-setwebproxystate", svc, "on")
		return err
	case "secureweb":
		if !st.Enabled {
			_, err := runNetworksetup("-setsecurewebproxystate", svc, "off")
			return err
		}
		if _, err := runNetworksetup("-setsecurewebproxy", svc, st.Server, st.Port); err != nil {
			return err
		}
		_, err := runNetworksetup("-setsecurewebproxystate", svc, "on")
		return err
	case "socks":
		if !st.Enabled {
			_, err := runNetworksetup("-setsocksfirewallproxystate", svc, "off")
			return err
		}
		if _, err := runNetworksetup("-setsocksfirewallproxy", svc, st.Server, st.Port); err != nil {
			return err
		}
		_, err := runNetworksetup("-setsocksfirewallproxystate", svc, "on")
		return err
	default:
		return fmt.Errorf("unknown proxy kind %q", kind)
	}
}

func resolveService(user string) (string, error) {
	if strings.TrimSpace(user) != "" {
		return user, nil
	}
	out, err := runNetworksetup("-listallnetworkservices")
	if err != nil {
		return "", fmt.Errorf("listallnetworkservices: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var names []string
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(line, "An asterisk") {
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}
		names = append(names, line)
	}
	for _, n := range names {
		if n == "Wi-Fi" {
			return n, nil
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("未找到可用网络服务，请用 -network-service 指定（networksetup -listallnetworkservices）")
	}
	return names[0], nil
}

func getProxyState(kind, svc string) (proxyState, error) {
	var flag string
	switch kind {
	case "web":
		flag = "-getwebproxy"
	case "secureweb":
		flag = "-getsecurewebproxy"
	case "socks":
		flag = "-getsocksfirewallproxy"
	default:
		return proxyState{}, fmt.Errorf("unknown kind %q", kind)
	}
	out, err := runNetworksetup(flag, svc)
	if err != nil {
		return proxyState{}, err
	}
	return parseProxyOutput(out), nil
}

func parseProxyOutput(out string) proxyState {
	var st proxyState
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Enabled:") {
			st.Enabled = strings.TrimSpace(strings.TrimPrefix(line, "Enabled:")) == "Yes"
		}
		if strings.HasPrefix(line, "Server:") {
			st.Server = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		}
		if strings.HasPrefix(line, "Port:") {
			st.Port = strings.TrimSpace(strings.TrimPrefix(line, "Port:"))
		}
	}
	return st
}

func runNetworksetup(args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("networksetup", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("networksetup %v: %w: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

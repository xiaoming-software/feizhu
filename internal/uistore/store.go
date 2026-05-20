// Package uistore 持久化 feizhu-client-ui 的服务器与登录信息（~/.feizhu/client-ui-settings.json）。
package uistore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	dirName  = ".feizhu"
	fileName = "client-ui-settings.json"
)

// Settings 上次成功登录时保存的表单内容。
type Settings struct {
	Host          string `json:"host"`
	Port          string `json:"port"`
	Password      string `json:"password"`
	UpstreamProxy string `json:"upstream_proxy,omitempty"`
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName, fileName), nil
}

// Load 读取已保存的设置；文件不存在时返回 nil, nil。
func Load() (*Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Host == "" && s.Port == "" && s.Password == "" {
		return nil, nil
	}
	return &s, nil
}

// Save 写入设置（权限 0600）。host/port/password 由调用方校验非空。
func Save(s *Settings) error {
	if s == nil {
		return errors.New("uistore: 设置为空")
	}
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

package uistore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if runtimeHome := os.Getenv("USERPROFILE"); runtimeHome != "" && dir != runtimeHome {
		// Windows 部分 API 读 USERPROFILE；测试中 HOME 通常足够
		t.Setenv("USERPROFILE", dir)
	}

	in := &Settings{Host: "1.2.3.4", Port: "8443", Password: "secret"}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("expected settings")
	}
	if out.Host != in.Host || out.Port != in.Port || out.Password != in.Password {
		t.Fatalf("got %+v want %+v", out, in)
	}
	path := filepath.Join(dir, dirName, fileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode want 0600 got %o", info.Mode().Perm())
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Fatalf("expected nil, got %+v", s)
	}
}

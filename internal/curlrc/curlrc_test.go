//go:build unix || windows

package curlrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRestoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Apply("http://127.0.0.1:7890"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".curlrc"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, beginMarker) || !strings.Contains(s, "proxy = ") {
		t.Fatalf("bad curlrc: %s", s)
	}

	if err := Restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".curlrc")); !os.IsNotExist(err) {
		t.Fatalf("empty curlrc should be removed: %v", err)
	}
}

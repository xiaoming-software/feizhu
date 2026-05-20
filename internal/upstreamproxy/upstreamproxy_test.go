package upstreamproxy

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	u, err := Parse("xiaoming:pass@15.235.183.47:2000")
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Hostname() != "15.235.183.47" || u.Port() != "2000" {
		t.Fatalf("unexpected url: %+v", u)
	}
	if u.User.Username() != "xiaoming" {
		t.Fatalf("user=%q", u.User.Username())
	}
}

func TestHostPortDefaults(t *testing.T) {
	u, _ := Parse("http://127.0.0.1")
	hp, err := HostPort(u)
	if err != nil {
		t.Fatal(err)
	}
	if hp != "127.0.0.1:80" {
		t.Fatalf("got %q", hp)
	}
}

func TestParseScheme(t *testing.T) {
	u, err := Parse("socks5://a:b@1.2.3.4:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(u.Scheme, "socks5") {
		t.Fatalf("scheme=%q", u.Scheme)
	}
}

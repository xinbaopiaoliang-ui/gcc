package main

import "testing"

func TestParseConnectTarget(t *testing.T) {
	host, port, err := parseConnectTarget("store.steampowered.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if host != "store.steampowered.com" || port != 443 {
		t.Fatalf("target = %s:%d, want store.steampowered.com:443", host, port)
	}

	host, port, err = parseConnectTarget("steamcommunity.com")
	if err != nil {
		t.Fatal(err)
	}
	if host != "steamcommunity.com" || port != 443 {
		t.Fatalf("target = %s:%d, want steamcommunity.com:443", host, port)
	}
}

func TestAllowRules(t *testing.T) {
	rules, err := parseAllowRules("steamcommunity.com,.steamcommunity.com,steampowered.com,.steampowered.com,steamserver.net,.steamserver.net", "443,27014-27050")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		host string
		port int
		want bool
	}{
		{host: "steamcommunity.com", port: 443, want: true},
		{host: "forums.steamcommunity.com", port: 443, want: true},
		{host: "store.steampowered.com", port: 443, want: true},
		{host: "cmp2-tyo3.steamserver.net", port: 27020, want: true},
		{host: "cmp2-tyo3.steamserver.net", port: 27051, want: false},
		{host: "example.com", port: 443, want: false},
		{host: "steamcommunity.com", port: 80, want: false},
		{host: "badsteamcommunity.com", port: 443, want: false},
	}

	for _, tc := range cases {
		got := rules.Allow(tc.host, tc.port)
		if got != tc.want {
			t.Fatalf("Allow(%q, %d) = %v, want %v", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestIsLoopbackListen(t *testing.T) {
	if !isLoopbackListen("127.0.0.1:18080") {
		t.Fatal("127.0.0.1 should be loopback")
	}
	if !isLoopbackListen("localhost:18080") {
		t.Fatal("localhost should be loopback")
	}
	if isLoopbackListen("0.0.0.0:18080") {
		t.Fatal("0.0.0.0 should not be loopback")
	}
}

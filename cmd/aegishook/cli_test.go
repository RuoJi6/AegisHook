package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestListenOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"defaults", nil, "127.0.0.1:18790"},
		{"port", []string{"--port", "18800"}, "127.0.0.1:18800"},
		{"localhost", []string{"serve", "--host", "localhost", "--port", "18800"}, "localhost:18800"},
		{"loopback", []string{"--host", "127.0.0.2"}, "127.0.0.2:18790"},
		{"ipv6", []string{"--host", "::1", "--port", "18800"}, "[::1]:18800"},
		{"legacy", []string{"--addr", "127.0.0.1:18800"}, "127.0.0.1:18800"},
		{"legacy ipv6", []string{"--addr", "[::1]:18800"}, "[::1]:18800"},
		{"status", []string{"status", "--host", "localhost", "--port", "18800"}, "localhost:18800"},
		{"install", []string{"install", "--agent", "claude", "--port", "18800"}, "127.0.0.1:18800"},
		{"uninstall", []string{"uninstall", "--id", "fixture", "--port", "18800"}, "127.0.0.1:18800"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseOptions(tc.args, io.Discard)
			if err != nil || o.addr != tc.want {
				t.Fatalf("got address %q, error %v; want %q", o.addr, err, tc.want)
			}
		})
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--host", "0.0.0.0"}, {"--host", "192.168.1.2"}, {"--host", "::"},
		{"--host", "example.com"}, {"--host", ""}, {"--host", "http://localhost"},
		{"--port", "0"}, {"--port", "-1"}, {"--port", "65536"}, {"--port", "abc"},
		{"--addr", "localhost"}, {"--addr", "::1:18800"}, {"--addr", "localhost:http"},
		{"--addr", "localhost:0"}, {"--addr", "localhost:65536"}, {"--addr", ":18800"},
		{"--addr", "localhost:18800", "--port", "18790"},
		{"--host", "127.0.0.1", "--addr", "localhost:18800"},
		{"serve", "extra"}, {""}, {"bogus"}, {"--unknown"},
		{"serve", "--public-url", "http://review.example"},
		{"serve", "--public-url", "https://review.example/path"},
		{"serve", "--trusted-proxies", "127.0.0.1"},
		{"serve", "--trusted-proxies", "127.0.0.1/32,invalid"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseOptions(args, io.Discard); err == nil {
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestRemoteServeOptionsKeepLoopback(t *testing.T) {
	o, err := parseOptions([]string{"serve", "--public-url", "https://review.example/", "--trusted-proxies", "127.0.0.1/32,::1/128", "--client-binaries", "/fixture/clients"}, io.Discard)
	if err != nil || o.addr != "127.0.0.1:18790" || o.publicURL != "https://review.example" || o.clientBinaries != "/fixture/clients" {
		t.Fatal("remote configuration changed listener or origin", err)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {"serve", "-h"}, {"install", "-h"}, {"uninstall", "--help"}, {"status", "-h"}, {"help", "install"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			_, err := parseOptions(args, &out)
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("help returned %v", err)
			}
			for _, want := range []string{"用法", "serve", "install", "status", "-host", "-port", "-addr", "--help", "示例"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help missing %q", want)
				}
			}
		})
	}
}

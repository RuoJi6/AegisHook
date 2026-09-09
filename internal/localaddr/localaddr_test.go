package localaddr

import "testing"

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST": true, "127.0.0.1": true, "127.0.0.2": true,
		"127.255.255.254": true, "::1": true, "0:0:0:0:0:0:0:1": true,
		"::ffff:127.0.0.2": true, "::ffff:7f00:2": true,
		"": false, "0.0.0.0": false, "::": false, "192.168.1.2": false,
		"example.com": false, "localhost.evil.example": false, "127.0.0.1.evil.example": false,
		"127.1": false, "::ffff:192.168.1.2": false, "::1%lo0": false,
	} {
		t.Run(host, func(t *testing.T) {
			if got := IsLoopback(host); got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

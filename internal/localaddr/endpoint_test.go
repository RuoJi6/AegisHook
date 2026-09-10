package localaddr

import "testing"

func TestClientEndpoint(t *testing.T) {
	for _, v := range []string{"https://review.example", "https://192.0.2.1:9443/", "http://127.0.0.1:18790", "http://[::1]:18790"} {
		if _, err := Endpoint(v); err != nil {
			t.Fatalf("valid endpoint %s: %v", v, err)
		}
	}
	for _, v := range []string{"", "http://192.0.2.1", "https://u:p@review.example", "https://review.example/path", "https://review.example?", "https://review.example#x", "https://review.example:0", "file:///etc"} {
		if _, err := Endpoint(v); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", v)
		}
	}
}

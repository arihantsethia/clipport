package main

import "testing"

func TestRequireLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:18765", "localhost:18765", "[::1]:18765"} {
		if err := requireLoopback(addr); err != nil {
			t.Fatalf("requireLoopback(%q) = %v", addr, err)
		}
	}
}

func TestRequireLoopbackRejectsPublicBind(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:18765", "192.168.1.5:18765", ":18765"} {
		if err := requireLoopback(addr); err == nil {
			t.Fatalf("requireLoopback(%q) succeeded, want error", addr)
		}
	}
}

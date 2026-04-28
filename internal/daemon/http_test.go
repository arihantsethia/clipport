package daemon

import (
	"net/http"
	"testing"
)

func TestAuthorizedRequiresBearerToken(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/v1/clipboard/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	if !authorized(req, "secret") {
		t.Fatal("bearer token was rejected")
	}
}

func TestAuthorizedRejectsRawToken(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/v1/clipboard/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "secret")
	if authorized(req, "secret") {
		t.Fatal("raw token was accepted")
	}
}

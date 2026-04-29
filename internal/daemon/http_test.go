package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arihantsethia/clipport/internal/clipboard"
)

func TestAuthorizedRequiresBearerToken(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/v1/clipboard", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	if !authorized(req, "secret") {
		t.Fatal("bearer token was rejected")
	}
}

func TestAuthorizedRejectsRawToken(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/v1/clipboard", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "secret")
	if authorized(req, "secret") {
		t.Fatal("raw token was accepted")
	}
}

func TestClipboardHTTPReturnsPNG(t *testing.T) {
	s := &Server{Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/clipboard", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	s.HTTPHandler("secret").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q", got)
	}
	if rec.Body.String() != "png" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestClipboardHTTPReturnsText(t *testing.T) {
	s := &Server{Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindText, Data: []byte("hello")}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/clipboard", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	s.HTTPHandler("secret").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

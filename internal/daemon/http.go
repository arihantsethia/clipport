package daemon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arihantsethia/clipport/internal/clipboard"
)

func (s *Server) ListenHTTP(addr, bearerToken string) error {
	return http.ListenAndServe(addr, s.HTTPHandler(bearerToken))
}

func (s *Server) HTTPHandler(bearerToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, bearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	})
	mux.HandleFunc("/v1/clipboard", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, bearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.Clipboard == nil {
			http.Error(w, "clipboard unavailable", http.StatusNotFound)
			return
		}
		item, err := s.Clipboard.Read()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if item.Kind == clipboard.KindText {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "image/png")
		}
		_, _ = w.Write(item.Data)
	})
	return mux
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	bearer, ok := strings.CutPrefix(auth, "Bearer ")
	return ok && bearer == token
}

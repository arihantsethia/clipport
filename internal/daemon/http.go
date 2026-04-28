package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) ListenHTTP(addr, bearerToken string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, bearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	})
	mux.HandleFunc("/v1/clipboard/png", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, bearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		data, err := s.Images.ReadPNG()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
	return http.ListenAndServe(addr, mux)
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	bearer, ok := strings.CutPrefix(auth, "Bearer ")
	return ok && bearer == token
}

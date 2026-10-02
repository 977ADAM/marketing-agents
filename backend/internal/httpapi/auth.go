package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuth оборачивает next HTTP basic-аутентификацией.
// /healthz всегда пропускается (для healthcheck без креды).
// Если user пустой — middleware пропускает всё (локалка/тесты).
func BasicAuth(user, pass string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user == "" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(p), []byte(pass)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="marketing-agents"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

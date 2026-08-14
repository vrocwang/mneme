package jsonrpc

import (
	"net"
	"net/http"
	"strings"
)

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
// Returns "" when the header is absent or malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// requireAuth wraps a handler so that only requests carrying a valid bearer
// token are served. It fails closed: when no pairing guard is configured the
// request is denied rather than allowed through.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorize(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

// authorize validates the request's bearer token against the pairing guard.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.guard == nil {
		http.Error(w, "authentication not configured", http.StatusInternalServerError)
		return false
	}
	token := bearerToken(r)
	if token == "" || !s.guard.ValidateConstantTime(token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// rateLimit wraps a handler with per-client-IP rate limiting. Requests that
// exceed the window are rejected with 429 before reaching the handler.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter != nil && !s.limiter.RecordAction(clientIP(r)) {
			s.log.Warn("jsonrpc rate limit exceeded", "remote", r.RemoteAddr)
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the IP portion of RemoteAddr (dropping the port).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

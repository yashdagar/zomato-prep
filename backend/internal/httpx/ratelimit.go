package httpx

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/yashdagar/zomato-prep/backend/internal/ratelimit"
)

func RateLimit(l ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-User-ID")

			if key == "" {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}

				key = "ip:" + host
			}

			if !l.Allow(key) {
				w.Header().Set("Retry-After", "1")
				WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Package httpapi exposes the monitor's liveness endpoint.
package httpapi

import "net/http"

// New returns the monitor's HTTP handler. The only endpoint is a liveness
// check; it deliberately does not report the target backend's health.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}

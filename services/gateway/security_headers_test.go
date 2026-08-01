package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecurityHeaders verifies the OWASP hardening headers are present on
// every response from the gateway.
func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	h := securityHeadersMiddleware(inner)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if v := rr.Header().Get("Content-Security-Policy"); v == "" {
		t.Error("missing Content-Security-Policy header")
	} else if v != "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'" {
		t.Errorf("unexpected CSP: %s", v)
	}
	if v := rr.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("expected X-Content-Type-Options nosniff, got %q", v)
	}
	if v := rr.Header().Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("expected X-Frame-Options DENY, got %q", v)
	}
	if v := rr.Header().Get("Referrer-Policy"); v != "no-referrer" {
		t.Errorf("expected Referrer-Policy no-referrer, got %q", v)
	}
	if v := rr.Header().Get("Permissions-Policy"); v == "" {
		t.Error("missing Permissions-Policy header")
	}
	// HSTS only on TLS
	if v := rr.Header().Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HSTS should not be set over plain HTTP, got %q", v)
	}
}

// TestSecurityHeaders_HSTSOnTLS verifies Strict-Transport-Security is present
// when the request arrives over TLS.
func TestSecurityHeaders_HSTSOnTLS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeadersMiddleware(inner)

	req := httptest.NewRequest("GET", "/health", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if v := rr.Header().Get("Strict-Transport-Security"); v != "max-age=63072000; includeSubDomains" {
		t.Errorf("expected HSTS over TLS, got %q", v)
	}
}

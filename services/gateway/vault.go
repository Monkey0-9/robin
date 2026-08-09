package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// VaultClient is a wrapper for HashiCorp Vault integration.
type VaultClient struct {
	addr     string
	token    string
	client   *http.Client
	cache    sync.Map
	cacheTTL time.Duration
}

func NewVaultClient() *VaultClient {
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" {
		slog.Warn("VAULT_ADDR not set, Vault integration disabled")
	}
	return &VaultClient{
		addr:     addr,
		token:    token,
		client:   &http.Client{Timeout: 5 * time.Second},
		cacheTTL: 5 * time.Minute,
	}
}

// vaultResponse represents the standard Vault API response envelope.
type vaultResponse struct {
	Data struct {
		Data map[string]interface{} `json:"data"`
	} `json:"data"`
}

// GetSecret fetches a secret from Vault. Falls back to environment variable for testing.
func (v *VaultClient) GetSecret(path, key string) (string, error) {
	// Check cache first
	cacheKey := path + ":" + key
	if val, ok := v.cache.Load(cacheKey); ok {
		if entry, ok := val.(cacheEntry); ok && time.Since(entry.fetched) < v.cacheTTL {
			return entry.value, nil
		}
	}

	if v.addr == "" {
		// Fallback to env var for testing
		envKey := fmt.Sprintf("VAULT_%s", strings.ToUpper(key))
		val := os.Getenv(envKey)
		if val != "" {
			return val, nil
		}
		return "", fmt.Errorf("Vault disabled and env fallback %s not found", envKey)
	}

	u := fmt.Sprintf("%s/v1/%s", strings.TrimRight(v.addr, "/"), strings.TrimLeft(path, "/"))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("Vault request create: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var vresp vaultResponse
	if err := json.NewDecoder(resp.Body).Decode(&vresp); err != nil {
		return "", fmt.Errorf("Vault response decode: %w", err)
	}

	raw, ok := vresp.Data.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in Vault path %q", key, path)
	}

	val := fmt.Sprintf("%v", raw)
	v.cache.Store(cacheKey, cacheEntry{value: val, fetched: time.Now()})
	return val, nil
}

type cacheEntry struct {
	value   string
	fetched time.Time
}

// vaultTransitRequest represents a transit engine request body
type vaultTransitRequest struct {
	Input string `json:"input"`
	Hmac  string `json:"hmac,omitempty"`
}

// vaultTransitResponse represents a transit engine response envelope
type vaultTransitResponse struct {
	Data struct {
		Hmac  string `json:"hmac"`
		Valid bool   `json:"valid"`
	} `json:"data"`
}

// SignData uses the Vault Transit engine to generate an HMAC for the given data.
func (v *VaultClient) SignData(keyID string, data []byte) ([]byte, error) {
	if v.addr == "" {
		return nil, fmt.Errorf("Vault disabled, cannot sign data")
	}

	reqBody := vaultTransitRequest{
		Input: base64.StdEncoding.EncodeToString(data),
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("%s/v1/transit/hmac/%s", strings.TrimRight(v.addr, "/"), keyID)
	req, err := http.NewRequest("POST", u, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("Vault transit hmac request create: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Vault transit hmac request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var vresp vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vresp); err != nil {
		return nil, fmt.Errorf("Vault transit response decode: %w", err)
	}

	return []byte(vresp.Data.Hmac), nil
}

// VerifySignature uses the Vault Transit engine to verify an HMAC.
func (v *VaultClient) VerifySignature(keyID string, data []byte, hmacBytes []byte) (bool, error) {
	if v.addr == "" {
		return false, fmt.Errorf("Vault disabled, cannot verify signature")
	}

	reqBody := vaultTransitRequest{
		Input: base64.StdEncoding.EncodeToString(data),
		Hmac:  string(hmacBytes), // Vault returns string like "vault:v1:..."
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return false, err
	}

	u := fmt.Sprintf("%s/v1/transit/verify/%s", strings.TrimRight(v.addr, "/"), keyID)
	req, err := http.NewRequest("POST", u, bytes.NewBuffer(b))
	if err != nil {
		return false, fmt.Errorf("Vault transit verify request create: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("Vault transit verify request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("Vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var vresp vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vresp); err != nil {
		return false, fmt.Errorf("Vault transit response decode: %w", err)
	}

	return vresp.Data.Valid, nil
}

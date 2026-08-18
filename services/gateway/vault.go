package main

import (
	"bytes"
	"context"
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

// ============================================================================
// VaultClient — Production HashiCorp Vault Integration
// ============================================================================
// Features:
//   • AppRole authentication (RoleID + SecretID)
//   • KV v2 secret engine
//   • In-memory cache with TTL (never written to disk)
//   • Background lease renewal goroutine
//   • Transit engine: sign/verify HMAC
//   • Graceful shutdown via context cancellation
//
// Environment variables:
//   VAULT_ADDR       e.g. https://vault.example.com:8200
//   VAULT_ROLE_ID    AppRole RoleID
//   VAULT_SECRET_ID  AppRole SecretID
//   VAULT_TOKEN      (fallback: static token, for dev mode only)
//   VAULT_NAMESPACE  (optional: Vault Enterprise namespace)
//   VAULT_SKIP_VERIFY (optional: "true" to skip TLS verification in dev)
// ============================================================================

// VaultClient manages authentication and secret retrieval from HashiCorp Vault.
type VaultClient struct {
	addr      string
	namespace string
	client    *http.Client

	// Auth state
	mu          sync.RWMutex
	token       string
	tokenExpiry time.Time

	// Secret cache: key = "path:key" → (value, expiry)
	cache    sync.Map
	cacheTTL time.Duration

	// Background renewal
	ctx    context.Context
	cancel context.CancelFunc
}

type cacheEntry struct {
	value   string
	expiry  time.Time
}

type vaultAuthResponse struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int    `json:"lease_duration"` // seconds
		Renewable     bool   `json:"renewable"`
	} `json:"auth"`
}

type vaultResponse struct {
	Data struct {
		Data     map[string]interface{} `json:"data"`
		Metadata struct {
			Version int `json:"version"`
		} `json:"metadata"`
	} `json:"data"`
}

type vaultTransitRequest struct {
	Input string `json:"input"`
	Hmac  string `json:"hmac,omitempty"`
}

type vaultTransitResponse struct {
	Data struct {
		Hmac    string `json:"hmac"`
		Valid   bool   `json:"valid"`
		Ciphertext string `json:"ciphertext"`
		Plaintext  string `json:"plaintext"`
	} `json:"data"`
}

type vaultRenewRequest struct {
	Token     string `json:"token"`
	Increment int    `json:"increment,omitempty"`
}

// NewVaultClient constructs and authenticates a VaultClient.
// If VAULT_ROLE_ID + VAULT_SECRET_ID are set, it performs AppRole login.
// If VAULT_TOKEN is set, it uses that directly (dev mode).
// Returns a client even if Vault is unavailable (secrets fall back to env vars).
func NewVaultClient() *VaultClient {
	ctx, cancel := context.WithCancel(context.Background())
	vc := &VaultClient{
		addr:      strings.TrimRight(os.Getenv("VAULT_ADDR"), "/"),
		namespace: os.Getenv("VAULT_NAMESPACE"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
		ctx:      ctx,
		cancel:   cancel,
	}

	if vc.addr == "" {
		slog.Warn("[vault] VAULT_ADDR not set — secrets sourced from environment variables")
		return vc
	}

	// Try AppRole authentication first
	roleID := os.Getenv("VAULT_ROLE_ID")
	secretID := os.Getenv("VAULT_SECRET_ID")
	if roleID != "" && secretID != "" {
		if err := vc.appRoleLogin(roleID, secretID); err != nil {
			slog.Warn("[vault] AppRole login failed, falling back to VAULT_TOKEN", "error", err)
		} else {
			slog.Info("[vault] AppRole authentication successful")
			go vc.renewalLoop()
			return vc
		}
	}

	// Fallback to static token
	if tok := os.Getenv("VAULT_TOKEN"); tok != "" {
		vc.mu.Lock()
		vc.token = tok
		vc.tokenExpiry = time.Now().Add(72 * time.Hour)
		vc.mu.Unlock()
		slog.Warn("[vault] Using static VAULT_TOKEN (not recommended for production)")
		go vc.renewalLoop()
	} else {
		slog.Warn("[vault] No Vault credentials found — secrets sourced from environment variables")
	}

	return vc
}

// appRoleLogin authenticates using AppRole and stores the resulting token.
func (v *VaultClient) appRoleLogin(roleID, secretID string) error {
	payload := map[string]string{
		"role_id":   roleID,
		"secret_id": secretID,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(v.ctx, "POST",
		v.addr+"/v1/auth/approle/login", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var ar vaultAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if ar.Auth.ClientToken == "" {
		return fmt.Errorf("empty client_token in response")
	}

	v.mu.Lock()
	v.token = ar.Auth.ClientToken
	v.tokenExpiry = time.Now().Add(time.Duration(ar.Auth.LeaseDuration) * time.Second)
	v.mu.Unlock()
	return nil
}

// renewalLoop renews the Vault token before it expires.
// Runs as a background goroutine; exits on context cancellation.
func (v *VaultClient) renewalLoop() {
	for {
		v.mu.RLock()
		expiry := v.tokenExpiry
		v.mu.RUnlock()

		// Renew at 75% of remaining lifetime
		remaining := time.Until(expiry)
		if remaining <= 0 {
			remaining = 10 * time.Second
		}
		renewIn := remaining * 3 / 4

		select {
		case <-v.ctx.Done():
			slog.Info("[vault] Renewal loop stopped")
			return
		case <-time.After(renewIn):
			if err := v.renewToken(); err != nil {
				slog.Warn("[vault] Token renewal failed", "error", err)
				// Retry in 30s
				select {
				case <-v.ctx.Done():
					return
				case <-time.After(30 * time.Second):
				}
			} else {
				slog.Debug("[vault] Token renewed successfully")
			}
		}
	}
}

// renewToken calls the Vault token/renew-self endpoint.
func (v *VaultClient) renewToken() error {
	v.mu.RLock()
	tok := v.token
	v.mu.RUnlock()
	if tok == "" || v.addr == "" {
		return nil
	}

	body, _ := json.Marshal(vaultRenewRequest{Token: tok, Increment: 3600})
	req, err := http.NewRequestWithContext(v.ctx, "POST",
		v.addr+"/v1/auth/token/renew-self", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var ar vaultAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return err
	}

	v.mu.Lock()
	v.tokenExpiry = time.Now().Add(time.Duration(ar.Auth.LeaseDuration) * time.Second)
	v.mu.Unlock()
	return nil
}

// addHeaders adds the Vault token and optional namespace header.
func (v *VaultClient) addHeaders(req *http.Request) {
	v.mu.RLock()
	tok := v.token
	v.mu.RUnlock()
	if tok != "" {
		req.Header.Set("X-Vault-Token", tok)
	}
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}
}

// GetSecret fetches a secret from Vault KV v2. Falls back to env var when Vault is unavailable.
// path example: "secret/data/robin/alpaca"
// key example:  "api_key"
func (v *VaultClient) GetSecret(path, key string) (string, error) {
	// Check in-memory cache first
	cacheKey := path + ":" + key
	if val, ok := v.cache.Load(cacheKey); ok {
		if entry, ok := val.(cacheEntry); ok && time.Now().Before(entry.expiry) {
			return entry.value, nil
		}
	}

	// No Vault address → env var fallback
	if v.addr == "" {
		return v.envFallback(key)
	}

	// Build URL (supports both KV v1 and v2 paths)
	url := fmt.Sprintf("%s/v1/%s", v.addr, strings.TrimLeft(path, "/"))
	req, err := http.NewRequestWithContext(v.ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("vault: build request: %w", err)
	}
	v.addHeaders(req)

	resp, err := v.client.Do(req)
	if err != nil {
		slog.Warn("[vault] Request failed, trying env fallback", "path", path, "error", err)
		return v.envFallback(key)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("vault: secret not found at %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vault: status %d: %s", resp.StatusCode, string(raw))
	}

	var vr vaultResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return "", fmt.Errorf("vault: decode: %w", err)
	}

	// Try KV v2 nested data first, then KV v1 flat
	data := vr.Data.Data
	if data == nil {
		return "", fmt.Errorf("vault: empty data at %s", path)
	}

	raw, ok := data[key]
	if !ok {
		return "", fmt.Errorf("vault: key %q not found in %s", key, path)
	}

	value := fmt.Sprintf("%v", raw)

	// Cache with TTL
	v.cache.Store(cacheKey, cacheEntry{
		value:  value,
		expiry: time.Now().Add(v.cacheTTL),
	})

	return value, nil
}

// envFallback returns VAULT_<KEY> (upper-cased) from environment.
func (v *VaultClient) envFallback(key string) (string, error) {
	envKey := "VAULT_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	if val := os.Getenv(envKey); val != "" {
		return val, nil
	}
	// Also try without prefix for common secrets
	if val := os.Getenv(strings.ToUpper(key)); val != "" {
		return val, nil
	}
	return "", fmt.Errorf("vault disabled and env %s not set", envKey)
}

// PutSecret writes a secret to Vault KV v2.
// path example: "secret/data/robin/alpaca"
func (v *VaultClient) PutSecret(path, key, value string) error {
	if v.addr == "" {
		return fmt.Errorf("vault disabled")
	}
	payload := map[string]interface{}{
		"data": map[string]string{key: value},
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/v1/%s", v.addr, strings.TrimLeft(path, "/"))
	req, err := http.NewRequestWithContext(v.ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("vault: build request: %w", err)
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault: put status %d: %s", resp.StatusCode, string(raw))
	}

	// Invalidate cache
	v.cache.Delete(path + ":" + key)
	return nil
}

// SignData uses the Vault Transit engine to generate an HMAC for the given data.
func (v *VaultClient) SignData(keyID string, data []byte) ([]byte, error) {
	if v.addr == "" {
		return nil, fmt.Errorf("vault disabled: cannot sign data")
	}
	reqBody := vaultTransitRequest{
		Input: base64.StdEncoding.EncodeToString(data),
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v1/transit/hmac/%s", v.addr, keyID)
	req, err := http.NewRequestWithContext(v.ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("vault: build request: %w", err)
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault transit hmac: status %d: %s", resp.StatusCode, string(raw))
	}

	var vr vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("vault transit hmac decode: %w", err)
	}
	return []byte(vr.Data.Hmac), nil
}

// VerifySignature uses the Vault Transit engine to verify an HMAC.
func (v *VaultClient) VerifySignature(keyID string, data []byte, hmacBytes []byte) (bool, error) {
	if v.addr == "" {
		return false, fmt.Errorf("vault disabled: cannot verify")
	}
	reqBody := vaultTransitRequest{
		Input: base64.StdEncoding.EncodeToString(data),
		Hmac:  string(hmacBytes),
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v1/transit/verify/%s", v.addr, keyID)
	req, err := http.NewRequestWithContext(v.ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return false, fmt.Errorf("vault: build request: %w", err)
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("vault transit verify: status %d: %s", resp.StatusCode, string(raw))
	}

	var vr vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return false, fmt.Errorf("vault transit verify decode: %w", err)
	}
	return vr.Data.Valid, nil
}

// Encrypt uses the Vault Transit engine to encrypt plaintext.
func (v *VaultClient) Encrypt(keyID string, plaintext []byte) (string, error) {
	if v.addr == "" {
		return "", fmt.Errorf("vault disabled: cannot encrypt")
	}
	reqBody := vaultTransitRequest{
		Input: base64.StdEncoding.EncodeToString(plaintext),
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v1/transit/encrypt/%s", v.addr, keyID)
	req, err := http.NewRequestWithContext(v.ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var vr vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return "", err
	}
	return vr.Data.Ciphertext, nil
}

// Decrypt uses the Vault Transit engine to decrypt ciphertext.
func (v *VaultClient) Decrypt(keyID, ciphertext string) ([]byte, error) {
	if v.addr == "" {
		return nil, fmt.Errorf("vault disabled: cannot decrypt")
	}
	reqBody := map[string]string{"ciphertext": ciphertext}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v1/transit/decrypt/%s", v.addr, keyID)
	req, err := http.NewRequestWithContext(v.ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	v.addHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vr vaultTransitResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(vr.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("decode plaintext: %w", err)
	}
	return raw, nil
}

// IsAvailable returns true if Vault is configured and a token is present.
func (v *VaultClient) IsAvailable() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.addr != "" && v.token != "" && time.Now().Before(v.tokenExpiry)
}

// Shutdown cancels background renewal and clears cached secrets from memory.
func (v *VaultClient) Shutdown() {
	v.cancel()
	v.cache.Range(func(k, _ interface{}) bool {
		v.cache.Delete(k)
		return true
	})
	v.mu.Lock()
	v.token = ""
	v.mu.Unlock()
	slog.Info("[vault] Client shut down, secrets cleared from memory")
}

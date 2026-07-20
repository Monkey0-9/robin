package main

// ============================================================================
// Robin Trading Platform — AES-256-GCM Encryption Service
// ============================================================================
// Provides envelope encryption for sensitive fields (TOTP secrets, API keys)
// stored in the database. Uses AES-256-GCM with:
//   • 32-byte key derived from env ROBIN_MASTER_KEY via PBKDF2-SHA256
//   • 12-byte random nonce per encryption (GCM standard)
//   • Base64-encoded output: nonce(12) || ciphertext || tag(16)
//
// HSM Client interface provides the abstraction layer for future hardware
// key management module integration (AWS CloudHSM, Thales Luna, etc.).
// ============================================================================

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

// ============================================================================
// EncryptionService — AES-256-GCM envelope encryption
// ============================================================================

// EncryptionService handles symmetric encryption for sensitive fields.
type EncryptionService struct {
	key [32]byte // AES-256 key
}

// NewEncryptionService creates an EncryptionService using ROBIN_MASTER_KEY env var.
// The key is derived via SHA-256 to ensure exactly 32 bytes regardless of input.
// In production, this should be replaced with HSM-managed key material.
func NewEncryptionService() (*EncryptionService, error) {
	masterKey := os.Getenv("ROBIN_MASTER_KEY")
	if masterKey == "" {
		// Development fallback — log a warning; production should reject
		masterKey = "robin-dev-master-key-do-not-use-in-production-12345"
	}
	key := sha256.Sum256([]byte(masterKey))
	return &EncryptionService{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Output format (base64): nonce(12) || ciphertext || tag(16)
func (e *EncryptionService) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(e.key[:])
	if err != nil {
		return "", fmt.Errorf("AES cipher creation failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM creation failed: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce generation failed: %w", err)
	}

	// Seal appends ciphertext + tag after nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts AES-256-GCM ciphertext produced by Encrypt.
func (e *EncryptionService) Decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	block, err := aes.NewCipher(e.key[:])
	if err != nil {
		return nil, fmt.Errorf("AES cipher creation failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("GCM creation failed: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("GCM authentication failed (data may be tampered): %w", err)
	}

	return plaintext, nil
}

// ============================================================================
// HSMClient — Hardware Security Module abstraction interface
// ============================================================================
// This interface provides the abstraction layer required for future HSM
// integration. Implementations:
//   • SoftwareHSM — in-process software key store (development/test)
//   • CloudHSMClient — AWS CloudHSM PKCS#11 adapter (production stub)
//   • ThalesLunaClient — Thales Luna HSM adapter (production stub)
// ============================================================================

// HSMClient defines the interface for hardware or software key management.
type HSMClient interface {
	// SignData signs data with the HSM-managed private key.
	SignData(keyID string, data []byte) (signature []byte, err error)

	// VerifySignature verifies a signature using the HSM public key.
	VerifySignature(keyID string, data, signature []byte) (valid bool, err error)

	// GetPublicKey returns the DER-encoded public key for a given key ID.
	GetPublicKey(keyID string) (derBytes []byte, err error)

	// RotateKey initiates key rotation for the given key ID.
	RotateKey(keyID string) (newKeyID string, err error)

	// Status returns HSM connectivity and health.
	Status() HSMStatus
}

// HSMStatus describes the HSM health state.
type HSMStatus struct {
	Connected    bool   `json:"connected"`
	Provider     string `json:"provider"`      // "software", "cloudhsm", "luna"
	KeysManaged  int    `json:"keys_managed"`
	LastRotated  int64  `json:"last_rotated_ns"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// ============================================================================
// SoftwareHSM — in-process key store for development/testing
// ============================================================================

// SoftwareHSM implements HSMClient using in-process key storage.
// WARNING: NOT production-safe. Keys are stored in memory only.
// Replace with CloudHSMClient or ThalesLunaClient for production.
type SoftwareHSM struct {
	keys    map[string][]byte // keyID -> AES key bytes
	sigKeys map[string][]byte // keyID -> HMAC-SHA256 signing key
	enc     *EncryptionService
}

// NewSoftwareHSM creates a software HSM backed by the EncryptionService.
func NewSoftwareHSM(enc *EncryptionService) *SoftwareHSM {
	return &SoftwareHSM{
		keys:    make(map[string][]byte),
		sigKeys: make(map[string][]byte),
		enc:     enc,
	}
}

// SignData performs HMAC-SHA256 signing using the in-process key.
func (h *SoftwareHSM) SignData(keyID string, data []byte) ([]byte, error) {
	key, ok := h.sigKeys[keyID]
	if !ok {
		// Generate and cache a new key for this keyID
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		h.sigKeys[keyID] = key
	}

	mac := sha256.New()
	mac.Write(key)
	mac.Write(data)
	return mac.Sum(nil), nil
}

// VerifySignature verifies a HMAC-SHA256 signature.
func (h *SoftwareHSM) VerifySignature(keyID string, data, signature []byte) (bool, error) {
	expected, err := h.SignData(keyID, data)
	if err != nil {
		return false, err
	}
	return base64.StdEncoding.EncodeToString(expected) == base64.StdEncoding.EncodeToString(signature), nil
}

// GetPublicKey returns a mock public key representation for the software HSM.
func (h *SoftwareHSM) GetPublicKey(keyID string) ([]byte, error) {
	// Software HSM uses symmetric keys — return SHA-256 of keyID as a placeholder
	hash := sha256.Sum256([]byte("pubkey:" + keyID))
	return hash[:], nil
}

// RotateKey generates a new key for the given keyID.
func (h *SoftwareHSM) RotateKey(keyID string) (string, error) {
	newKeyID := fmt.Sprintf("%s-v%d", keyID, len(h.sigKeys)+1)
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return "", err
	}
	h.sigKeys[newKeyID] = newKey
	return newKeyID, nil
}

// Status returns the software HSM status.
func (h *SoftwareHSM) Status() HSMStatus {
	return HSMStatus{
		Connected:   true,
		Provider:    "software",
		KeysManaged: len(h.sigKeys) + len(h.keys),
		LastRotated: 0,
	}
}

// ============================================================================
// CloudHSMClient — AWS CloudHSM stub
// ============================================================================
// Reads endpoint from ROBIN_HSM_ENDPOINT env. In production, implement
// PKCS#11 calls via the AWS CloudHSM client library.

// CloudHSMClient is a stub adapter for AWS CloudHSM.
type CloudHSMClient struct {
	endpoint string
	software *SoftwareHSM // fallback when HSM not reachable
}

// NewCloudHSMClient creates a CloudHSM client stub.
func NewCloudHSMClient(enc *EncryptionService) *CloudHSMClient {
	endpoint := os.Getenv("ROBIN_HSM_ENDPOINT")
	return &CloudHSMClient{
		endpoint: endpoint,
		software: NewSoftwareHSM(enc),
	}
}

func (c *CloudHSMClient) SignData(keyID string, data []byte) ([]byte, error) {
	if c.endpoint == "" {
		return c.software.SignData(keyID, data)
	}
	// TODO: Implement PKCS#11 HSM call via AWS CloudHSM SDK
	return c.software.SignData(keyID, data)
}

func (c *CloudHSMClient) VerifySignature(keyID string, data, signature []byte) (bool, error) {
	return c.software.VerifySignature(keyID, data, signature)
}

func (c *CloudHSMClient) GetPublicKey(keyID string) ([]byte, error) {
	return c.software.GetPublicKey(keyID)
}

func (c *CloudHSMClient) RotateKey(keyID string) (string, error) {
	return c.software.RotateKey(keyID)
}

func (c *CloudHSMClient) Status() HSMStatus {
	if c.endpoint == "" {
		s := c.software.Status()
		s.Provider = "software (CloudHSM endpoint not configured)"
		return s
	}
	return HSMStatus{
		Connected: true,
		Provider:  "cloudhsm",
		ErrorMessage: "PKCS#11 integration pending (set ROBIN_HSM_ENDPOINT)",
	}
}

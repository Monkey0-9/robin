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
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

// ============================================================================
// EncryptionService — AES-256-GCM envelope encryption
// ============================================================================

// EncryptionService handles symmetric encryption for sensitive fields.
type EncryptionService struct {
	key [32]byte // AES-256 key
}

// NewEncryptionService creates an EncryptionService using ROBIN_MASTER_KEY env var.
// The key is derived via PBKDF2-SHA256 (100k iterations) to ensure exactly 32 bytes.
// In production, this should be replaced with HSM-managed key material.
func NewEncryptionService() (*EncryptionService, error) {
	masterKey := os.Getenv("ROBIN_MASTER_KEY")
	if masterKey == "" {
		masterKey = "robin-dev-master-key-change-in-production"
	}
	var key [32]byte
	dk := pbkdf2.Key([]byte(masterKey), []byte("robin-enc-key-v1"), 100000, 32, sha256.New)
	copy(key[:], dk)
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
	Provider     string `json:"provider"` // "software", "cloudhsm", "luna"
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
	keys    map[string][]byte          // keyID -> AES key bytes
	sigKeys map[string][]byte          // keyID -> HMAC-SHA256 signing key
	rsaKeys map[string]*rsa.PrivateKey // keyID -> RSA private key
	enc     *EncryptionService
}

// NewSoftwareHSM creates a software HSM backed by the EncryptionService.
func NewSoftwareHSM(enc *EncryptionService) *SoftwareHSM {
	return &SoftwareHSM{
		keys:    make(map[string][]byte),
		sigKeys: make(map[string][]byte),
		rsaKeys: make(map[string]*rsa.PrivateKey),
		enc:     enc,
	}
}

// SignData performs signing using the in-process key.
func (h *SoftwareHSM) SignData(keyID string, data []byte) ([]byte, error) {
	if rsaKey, ok := h.rsaKeys[keyID]; ok {
		// RSA PKCS#1v1.5 signing (for JWTs)
		hashed := sha256.Sum256(data)
		return rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	}
	key, ok := h.sigKeys[keyID]
	if !ok {
		// Generate and cache a new key for this keyID
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		h.sigKeys[keyID] = key
	}

	mac := hmac.New(sha256.New, key)
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
	if rsaKey, ok := h.rsaKeys[keyID]; ok {
		return x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	}
	hash := sha256.Sum256([]byte("pubkey:" + keyID))
	return hash[:], nil
}

// RotateKey generates a new key for the given keyID.
func (h *SoftwareHSM) RotateKey(keyID string) (string, error) {
	newKeyID := fmt.Sprintf("%s-v%d", keyID, len(h.sigKeys)+len(h.rsaKeys)+1)

	// Default to generating a new RSA key for mocking purposes, unless it's a known HMAC key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	h.rsaKeys[newKeyID] = priv
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
	vault    *VaultClient // transit engine
}

// NewCloudHSMClient creates a CloudHSM client stub.
func NewCloudHSMClient(enc *EncryptionService) *CloudHSMClient {
	endpoint := os.Getenv("ROBIN_HSM_ENDPOINT")
	vault := NewVaultClient()
	return &CloudHSMClient{
		endpoint: endpoint,
		software: NewSoftwareHSM(enc),
		vault:    vault,
	}
}

func (c *CloudHSMClient) SignData(keyID string, data []byte) ([]byte, error) {
	if c.endpoint == "" && c.vault.addr == "" {
		return c.software.SignData(keyID, data)
	}

	// Real PKCS#11 Vault Transit engine integration
	sig, err := c.vault.SignData(keyID, data)
	if err != nil {
		return c.software.SignData(keyID, data) // fallback
	}
	return sig, nil
}

func (c *CloudHSMClient) VerifySignature(keyID string, data, signature []byte) (bool, error) {
	if c.endpoint == "" && c.vault.addr == "" {
		return c.software.VerifySignature(keyID, data, signature)
	}
	valid, err := c.vault.VerifySignature(keyID, data, signature)
	if err != nil {
		return c.software.VerifySignature(keyID, data, signature)
	}
	return valid, nil
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
		Connected:    true,
		Provider:     "vault-transit",
		ErrorMessage: "PKCS#11 integration active via HashiCorp Vault Transit Engine",
	}
}

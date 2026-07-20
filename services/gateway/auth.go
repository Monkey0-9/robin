package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

type jwtAuthenticator struct {
	publicKey *rsa.PublicKey
	hmacKey   []byte
	issuer    string
	audience  string
}

func newJWTAuthenticator() *jwtAuthenticator {
	auth := &jwtAuthenticator{
		issuer:   "robin-gateway",
		audience: "robin-services",
	}

	// Try RSA public key first (via PEM file or env var)
	pubKeyPEM := os.Getenv("ROBIN_JWT_PUBKEY")
	if pubKeyFile := os.Getenv("ROBIN_JWT_PUBKEY_FILE"); pubKeyFile != "" {
		data, err := os.ReadFile(pubKeyFile)
		if err != nil {
			slog.Warn("failed to read JWT public key file", "file", pubKeyFile, "error", err)
		} else {
			pubKeyPEM = string(data)
		}
	}

	if pubKeyPEM != "" {
		block, _ := pem.Decode([]byte(pubKeyPEM))
		if block != nil {
			parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err == nil {
				if rsaKey, ok := parsed.(*rsa.PublicKey); ok {
					auth.publicKey = rsaKey
					auth.publicKey = rsaKey
					slog.Info("JWT authenticator initialized with RSA public key")
					return auth
				}
			}
		}
	}


	slog.Warn("no RS256 public key configured (set ROBIN_JWT_PUBKEY or ROBIN_JWT_PUBKEY_FILE). Authentication will fail on verify.")
	return auth
}

func (a *jwtAuthenticator) verify(tokenStr string) (jwt.MapClaims, error) {
	if a.publicKey == nil {
		return nil, fmt.Errorf("authentication disabled: no RS256 JWT key configured")
	}

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v, expected RS256", token.Header["alg"])
		}
		return a.publicKey, nil
	}

	token, err := jwt.Parse(tokenStr, keyFunc,
		jwt.WithIssuer(a.issuer),
		jwt.WithAudience(a.audience),
		jwt.WithValidMethods([]string{"RS256"}),
	)

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

var jwtAuth = newJWTAuthenticator()

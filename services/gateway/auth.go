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
	useHMAC   bool
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
					auth.useHMAC = false
					slog.Info("JWT authenticator initialized with RSA public key")
					return auth
				}
			}
		}
	}

	// Fallback to HMAC with ROBIN_GATEWAY_API_TOKEN
	hmacSecret := os.Getenv("ROBIN_GATEWAY_API_TOKEN")
	if hmacSecret != "" {
		auth.hmacKey = []byte(hmacSecret)
		auth.useHMAC = true
		slog.Info("JWT authenticator initialized with HMAC key")
		return auth
	}

	slog.Warn("no JWT key configured (set ROBIN_JWT_PUBKEY or ROBIN_GATEWAY_API_TOKEN): auth disabled")
	return auth
}

func (a *jwtAuthenticator) verify(tokenStr string) (jwt.MapClaims, error) {
	if a.publicKey == nil && a.hmacKey == nil {
		// Dev mode — skip verification
		claims := jwt.MapClaims{}
		_, _, err := jwt.NewParser().ParseUnverified(tokenStr, &claims)
		return claims, err
	}

	var keyFunc jwt.Keyfunc
	if a.useHMAC {
		keyFunc = func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return a.hmacKey, nil
		}
	} else {
		keyFunc = func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v, expected RS256", token.Header["alg"])
			}
			return a.publicKey, nil
		}
	}

	token, err := jwt.Parse(tokenStr, keyFunc,
		jwt.WithIssuer(a.issuer),
		jwt.WithAudience(a.audience),
		jwt.WithValidMethods([]string{"RS256", "HS256", "HS384", "HS512"}),
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

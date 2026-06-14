// Package auth provides minimal session-token issue/verify for Stage 0. It signs a
// compact HMAC token carrying the user and org (tenant). Full OAuth sign-in and
// enterprise SSO/SAML are layered on later (ROADMAP Stage 0.5 / Stage 24); this is
// the primitive everything else builds on.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken is returned when a token fails verification.
var ErrInvalidToken = errors.New("auth: invalid token")

// Claims is the authenticated session identity. OrgID is the tenant id.
type Claims struct {
	UserID    string `json:"uid"`
	OrgID     string `json:"org"`
	ExpiresAt int64  `json:"exp"`
}

// Issue creates a signed token for the claims, valid for ttl.
func Issue(signingKey string, c Claims, ttl time.Duration) (string, error) {
	c.ExpiresAt = time.Now().Add(ttl).Unix()
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("auth issue: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + sign(signingKey, body), nil
}

// Verify checks the signature and expiry and returns the claims.
func Verify(signingKey, token string) (Claims, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(sign(signingKey, body))) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if time.Now().Unix() > c.ExpiresAt {
		return Claims{}, ErrInvalidToken
	}
	return c, nil
}

func sign(key, body string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

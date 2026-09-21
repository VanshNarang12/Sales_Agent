package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
)

var ErrGoogleToken = errors.New("identity: invalid google id token")

// GoogleIdentity is the verified subset of a Google ID token we act on.
type GoogleIdentity struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
}

// GoogleVerifier validates Google ID tokens against Google's JWKS. The audience is
// checked manually because we accept several client ids (web + desktop).
type GoogleVerifier struct {
	verifier  *oidc.IDTokenVerifier
	clientIDs []string
}

func NewGoogleVerifier(ctx context.Context, clientIDs []string) (*GoogleVerifier, error) {
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google oidc provider: %w", err)
	}
	return &GoogleVerifier{
		verifier:  provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		clientIDs: clientIDs,
	}, nil
}

func (g *GoogleVerifier) Verify(ctx context.Context, rawIDToken string) (GoogleIdentity, error) {
	tok, err := g.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("%w: %v", ErrGoogleToken, err)
	}
	if !slices.ContainsFunc(g.clientIDs, func(id string) bool { return slices.Contains(tok.Audience, id) }) {
		return GoogleIdentity{}, fmt.Errorf("%w: audience not allowed", ErrGoogleToken)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := tok.Claims(&claims); err != nil {
		return GoogleIdentity{}, fmt.Errorf("%w: claims: %v", ErrGoogleToken, err)
	}
	return GoogleIdentity{Sub: tok.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name}, nil
}

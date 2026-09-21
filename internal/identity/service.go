package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

var (
	ErrInvalidCredentials = errors.New("identity: invalid email or password")
	ErrInvalidEmail       = errors.New("identity: invalid email")
	ErrWeakPassword       = errors.New("identity: password must be at least 8 characters")
	ErrInvalidPhone       = errors.New("identity: phone must be E.164, e.g. +919876543210")
	ErrAlreadyVerified    = errors.New("identity: phone already verified")
	ErrGoogleDisabled     = errors.New("identity: google sign-in not configured")
	ErrOTPUnavailable     = errors.New("identity: OTP service unavailable")
)

var (
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRe = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)
)

// OrgOption is one candidate workspace when an email exists in several orgs.
type OrgOption struct {
	OrgID   string `json:"org_id"`
	OrgName string `json:"org_name"`
}

// OrgSelectionError: the password matched accounts in more than one org — the
// client must re-submit login with the chosen org_id.
type OrgSelectionError struct{ Options []OrgOption }

func (e *OrgSelectionError) Error() string {
	return "identity: email has accounts in multiple workspaces; pass org_id"
}

// ErrEmailAmbiguous: Google sign-in can't pick between several password accounts
// sharing the email — the user must log in with email+password instead.
var ErrEmailAmbiguous = errors.New("identity: email exists in multiple workspaces; use password login")

// Storage is what the flows need from the DB layer (implemented by *Store).
type Storage interface {
	CreateOrgWithUser(ctx context.Context, orgName, email, passwordHash, googleSub, provider string) (User, error)
	FindAllByEmail(ctx context.Context, email string) ([]User, error)
	FindByGoogleSub(ctx context.Context, sub string) (User, error)
	GetUser(ctx context.Context, userID string) (User, error)
	LinkGoogle(ctx context.Context, userID, sub string) error
	SetPhoneVerified(ctx context.Context, userID, phone string) error
	InsertRefresh(ctx context.Context, jti, orgID, userID string, expiresAt time.Time) error
	RotateRefresh(ctx context.Context, oldJTI, newJTI string, newExpiry time.Time) error
	RevokeRefresh(ctx context.Context, jti string) error
}

// GoogleTokenVerifier validates a Google ID token (implemented by *GoogleVerifier).
type GoogleTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (GoogleIdentity, error)
}

// Config holds token lifetimes.
type Config struct {
	AccessTTL  time.Duration
	PreAuthTTL time.Duration
	RefreshTTL time.Duration
}

// Session is what a successful auth call returns. Scope "pre" means the phone is
// not verified yet: RefreshToken is empty and only OTP endpoints + /v1/me work.
type Session struct {
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	ExpiresIn     int64  `json:"expires_in"`
	Scope         string `json:"scope"`
	PhoneVerified bool   `json:"phone_verified"`
}

// Service wires the auth flows together.
type Service struct {
	store      Storage
	otp        *OTPManager
	sender     OTPSender
	google     GoogleTokenVerifier
	signingKey string
	cfg        Config
	log        *slog.Logger
}

func NewService(store Storage, otp *OTPManager, sender OTPSender, google GoogleTokenVerifier, signingKey string, cfg Config, log *slog.Logger) *Service {
	return &Service{store: store, otp: otp, sender: sender, google: google, signingKey: signingKey, cfg: cfg, log: log}
}

// Signup creates a new org with its first user (one signup = one new tenant; team
// invites arrive in Stage 18) and returns a pre-scope session.
func (s *Service) Signup(ctx context.Context, email, password, orgName string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) {
		return Session{}, ErrInvalidEmail
	}
	if len(password) < 8 {
		return Session{}, ErrWeakPassword
	}
	if orgName = strings.TrimSpace(orgName); orgName == "" {
		orgName = defaultOrgName(email)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Session{}, err
	}
	u, err := s.store.CreateOrgWithUser(ctx, orgName, email, hash, "", "")
	if err != nil {
		return Session{}, err
	}
	s.log.Info("signup", "org", u.OrgID, "user", u.ID)
	return s.sessionFor(ctx, u)
}

// Login authenticates email+password. The same email may hold accounts in several
// orgs: the password is checked against each; on several matches the caller gets
// an OrgSelectionError and retries with orgID set.
func (s *Service) Login(ctx context.Context, email, password, orgID string) (Session, error) {
	users, err := s.store.FindAllByEmail(ctx, email)
	if err != nil {
		return Session{}, err
	}
	var matches []User
	for _, u := range users {
		if u.PasswordHash != "" && VerifyPassword(u.PasswordHash, password) {
			matches = append(matches, u)
		}
	}
	if len(matches) == 0 {
		if len(users) == 0 {
			// Burn comparable time so missing vs. wrong-password is not observable.
			VerifyPassword("$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", password)
		}
		return Session{}, ErrInvalidCredentials
	}
	if orgID != "" {
		for _, u := range matches {
			if u.OrgID == orgID {
				return s.sessionFor(ctx, u)
			}
		}
		return Session{}, ErrInvalidCredentials
	}
	if len(matches) > 1 {
		sel := &OrgSelectionError{}
		for _, u := range matches {
			sel.Options = append(sel.Options, OrgOption{OrgID: u.OrgID, OrgName: u.OrgName})
		}
		return Session{}, sel
	}
	return s.sessionFor(ctx, matches[0])
}

// GoogleAuth signs a user in (or up) from a client-obtained Google ID token.
func (s *Service) GoogleAuth(ctx context.Context, rawIDToken, orgName string) (Session, error) {
	if s.google == nil {
		return Session{}, ErrGoogleDisabled
	}
	gid, err := s.google.Verify(ctx, rawIDToken)
	if err != nil {
		return Session{}, err
	}
	if u, err := s.store.FindByGoogleSub(ctx, gid.Sub); err == nil {
		return s.sessionFor(ctx, u)
	} else if !errors.Is(err, ErrUserNotFound) {
		return Session{}, err
	}
	// Known email from a manual signup → link the Google identity, but only when
	// it's unambiguous (exactly one account holds the email).
	existing, err := s.store.FindAllByEmail(ctx, gid.Email)
	if err != nil {
		return Session{}, err
	}
	if len(existing) > 1 {
		return Session{}, ErrEmailAmbiguous
	}
	if len(existing) == 1 {
		u := existing[0]
		if err := s.store.LinkGoogle(tenancy.With(ctx, tenancy.TenantID(u.OrgID)), u.ID, gid.Sub); err != nil {
			return Session{}, err
		}
		return s.sessionFor(ctx, u)
	}
	if !gid.EmailVerified {
		return Session{}, fmt.Errorf("%w: email not verified by google", ErrGoogleToken)
	}
	if orgName = strings.TrimSpace(orgName); orgName == "" {
		orgName = defaultOrgName(gid.Email)
	}
	u, err := s.store.CreateOrgWithUser(ctx, orgName, gid.Email, "", gid.Sub, "google")
	if err != nil {
		return Session{}, err
	}
	s.log.Info("google signup", "org", u.OrgID, "user", u.ID)
	return s.sessionFor(ctx, u)
}

// SendOTP issues a code for the claimed user and delivers it over WhatsApp.
func (s *Service) SendOTP(ctx context.Context, claims auth.Claims, phone string) error {
	if s.otp == nil || s.sender == nil {
		return ErrOTPUnavailable
	}
	phone = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(phone))
	if !phoneRe.MatchString(phone) {
		return ErrInvalidPhone
	}
	tctx := tenancy.With(ctx, tenancy.TenantID(claims.OrgID))
	u, err := s.store.GetUser(tctx, claims.UserID)
	if err != nil {
		return err
	}
	if u.PhoneVerifiedAt != nil {
		return ErrAlreadyVerified
	}
	code, err := s.otp.Issue(ctx, u.ID, phone)
	if err != nil {
		return err
	}
	// On failure the code + cooldown are kept: a timed-out send may still have been
	// delivered, so wiping would invalidate a code the user is about to type.
	if err := s.sender.SendCode(ctx, phone, code); err != nil {
		return fmt.Errorf("otp delivery: %w", err)
	}
	s.log.Info("otp sent", "org", u.OrgID, "user", u.ID, "phone_last4", last4(phone))
	return nil
}

// VerifyOTP checks the code, stamps the verified phone, and upgrades to full scope.
func (s *Service) VerifyOTP(ctx context.Context, claims auth.Claims, code string) (Session, error) {
	if s.otp == nil {
		return Session{}, ErrOTPUnavailable
	}
	phone, err := s.otp.Verify(ctx, claims.UserID, code)
	if err != nil {
		return Session{}, err
	}
	tctx := tenancy.With(ctx, tenancy.TenantID(claims.OrgID))
	if err := s.store.SetPhoneVerified(tctx, claims.UserID, phone); err != nil {
		return Session{}, err
	}
	u, err := s.store.GetUser(tctx, claims.UserID)
	if err != nil {
		return Session{}, err
	}
	s.log.Info("phone verified", "org", u.OrgID, "user", u.ID, "phone_last4", last4(phone))
	return s.sessionFor(ctx, u)
}

// Refresh rotates a refresh token and returns a fresh full session.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	claims, err := auth.Verify(s.signingKey, refreshToken)
	if err != nil || claims.Scope != auth.ScopeRefresh || claims.TokenID == "" {
		return Session{}, auth.ErrInvalidToken
	}
	newJTI := uuid.NewString()
	tctx := tenancy.With(ctx, tenancy.TenantID(claims.OrgID))
	if err := s.store.RotateRefresh(tctx, claims.TokenID, newJTI, time.Now().Add(s.cfg.RefreshTTL)); err != nil {
		return Session{}, err
	}
	return s.issuePair(claims.UserID, claims.OrgID, newJTI)
}

// Logout revokes the presented refresh token. Idempotent.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	claims, err := auth.Verify(s.signingKey, refreshToken)
	if err != nil || claims.TokenID == "" {
		return auth.ErrInvalidToken
	}
	tctx := tenancy.With(ctx, tenancy.TenantID(claims.OrgID))
	return s.store.RevokeRefresh(tctx, claims.TokenID)
}

// Me returns the profile for the authenticated claims.
func (s *Service) Me(ctx context.Context, claims auth.Claims) (User, error) {
	tctx := tenancy.With(ctx, tenancy.TenantID(claims.OrgID))
	return s.store.GetUser(tctx, claims.UserID)
}

// sessionFor issues a pre token (phone unverified) or a full access+refresh pair.
func (s *Service) sessionFor(ctx context.Context, u User) (Session, error) {
	if u.PhoneVerifiedAt == nil {
		tok, err := auth.Issue(s.signingKey, auth.Claims{UserID: u.ID, OrgID: u.OrgID, Scope: auth.ScopePre}, s.cfg.PreAuthTTL)
		if err != nil {
			return Session{}, err
		}
		return Session{AccessToken: tok, ExpiresIn: int64(s.cfg.PreAuthTTL.Seconds()), Scope: auth.ScopePre}, nil
	}
	jti := uuid.NewString()
	tctx := tenancy.With(ctx, tenancy.TenantID(u.OrgID))
	if err := s.store.InsertRefresh(tctx, jti, u.OrgID, u.ID, time.Now().Add(s.cfg.RefreshTTL)); err != nil {
		return Session{}, err
	}
	return s.issuePair(u.ID, u.OrgID, jti)
}

func (s *Service) issuePair(userID, orgID, jti string) (Session, error) {
	access, err := auth.Issue(s.signingKey, auth.Claims{UserID: userID, OrgID: orgID, Scope: auth.ScopeFull}, s.cfg.AccessTTL)
	if err != nil {
		return Session{}, err
	}
	refresh, err := auth.Issue(s.signingKey, auth.Claims{UserID: userID, OrgID: orgID, Scope: auth.ScopeRefresh, TokenID: jti}, s.cfg.RefreshTTL)
	if err != nil {
		return Session{}, err
	}
	return Session{
		AccessToken:   access,
		RefreshToken:  refresh,
		ExpiresIn:     int64(s.cfg.AccessTTL.Seconds()),
		Scope:         auth.ScopeFull,
		PhoneVerified: true,
	}, nil
}

// defaultOrgName labels a solo signup's workspace from the email's local part
// ("vansh@gmail.com" → "vansh's workspace"). A label only — isolation is by UUID.
func defaultOrgName(email string) string {
	local := email
	if i := strings.Index(email, "@"); i > 0 {
		local = email[:i]
	}
	return local + "'s workspace"
}

func last4(phone string) string {
	if len(phone) <= 4 {
		return phone
	}
	return phone[len(phone)-4:]
}

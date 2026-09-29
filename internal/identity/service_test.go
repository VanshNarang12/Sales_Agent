package identity

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
)

// fakeStore is an in-memory Storage for flow tests.
type fakeStore struct {
	users   map[string]*User // by id
	refresh map[string]*refreshRow
}

type refreshRow struct {
	orgID, userID string
	expiresAt     time.Time
	rotated       bool
	revoked       bool
	replacedBy    string
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]*User{}, refresh: map[string]*refreshRow{}}
}

func (f *fakeStore) CreateOrgWithUser(_ context.Context, orgName, email, hash, sub, _ string) (User, error) {
	u := User{ID: uuid.NewString(), OrgID: uuid.NewString(), OrgName: orgName, Email: email, Role: "admin", PasswordHash: hash, GoogleSub: sub}
	f.users[u.ID] = &u
	return u, nil
}

func (f *fakeStore) FindAllByEmail(_ context.Context, email string) ([]User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var out []User
	for _, u := range f.users {
		if u.Email == email {
			out = append(out, *u)
		}
	}
	return out, nil
}

func (f *fakeStore) FindByGoogleSub(_ context.Context, sub string) (User, error) {
	for _, u := range f.users {
		if u.GoogleSub == sub {
			return *u, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (f *fakeStore) GetUser(_ context.Context, id string) (User, error) {
	if u, ok := f.users[id]; ok {
		return *u, nil
	}
	return User{}, ErrUserNotFound
}

func (f *fakeStore) LinkGoogle(_ context.Context, id, sub string) error {
	f.users[id].GoogleSub = sub
	return nil
}

func (f *fakeStore) SetPhoneVerified(_ context.Context, id, phone string) error {
	for _, u := range f.users {
		if u.Phone == phone && u.ID != id {
			return ErrPhoneTaken
		}
	}
	now := time.Now()
	f.users[id].Phone = phone
	f.users[id].PhoneVerifiedAt = &now
	return nil
}

func (f *fakeStore) InsertRefresh(_ context.Context, jti, orgID, userID string, exp time.Time) error {
	f.refresh[jti] = &refreshRow{orgID: orgID, userID: userID, expiresAt: exp}
	return nil
}

func (f *fakeStore) RotateRefresh(_ context.Context, oldJTI, newJTI string, exp time.Time) error {
	row, ok := f.refresh[oldJTI]
	if !ok || row.rotated || row.revoked || time.Now().After(row.expiresAt) {
		if ok {
			// mimic chain revocation
			for j := oldJTI; j != ""; j = f.refresh[j].replacedBy {
				f.refresh[j].revoked = true
			}
		}
		return ErrTokenReuse
	}
	row.rotated, row.replacedBy = true, newJTI
	f.refresh[newJTI] = &refreshRow{orgID: row.orgID, userID: row.userID, expiresAt: exp}
	return nil
}

func (f *fakeStore) RevokeRefresh(_ context.Context, jti string) error {
	if r, ok := f.refresh[jti]; ok {
		r.revoked = true
	}
	return nil
}

type fakeSender struct {
	lastPhone, lastCode string
	fail                bool
}

func (f *fakeSender) SendCode(_ context.Context, phone, code string) error {
	if f.fail {
		return errors.New("meta down")
	}
	f.lastPhone, f.lastCode = phone, code
	return nil
}

type fakeGoogle struct{ id GoogleIdentity }

func (f fakeGoogle) Verify(context.Context, string) (GoogleIdentity, error) { return f.id, nil }

func newTestService(t *testing.T, store Storage, sender OTPSender, google GoogleTokenVerifier) *Service {
	t.Helper()
	mr := miniredis.RunT(t)
	otp := NewOTPManager(redis.NewClient(&redis.Options{Addr: mr.Addr()}), OTPConfig{
		TTL: 5 * time.Minute, MaxAttempts: 3, ResendCooldown: time.Minute, DailyCap: 5,
	})
	return NewService(store, otp, sender, google, "test-signing-key", Config{
		AccessTTL: 15 * time.Minute, PreAuthTTL: 10 * time.Minute, RefreshTTL: 30 * 24 * time.Hour,
	}, slog.Default())
}

func TestSignupReturnsPreSession(t *testing.T) {
	s := newTestService(t, newFakeStore(), &fakeSender{}, nil)
	sess, err := s.Signup(context.Background(), "vansh@gmail.com", "password123", "")
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if sess.Scope != auth.ScopePre || sess.RefreshToken != "" || sess.PhoneVerified {
		t.Fatalf("want pre session without refresh token, got %+v", sess)
	}
	claims, err := auth.Verify("test-signing-key", sess.AccessToken)
	if err != nil || claims.Scope != auth.ScopePre {
		t.Fatalf("token claims = %+v, %v; want pre scope", claims, err)
	}
}

func TestSignupValidation(t *testing.T) {
	s := newTestService(t, newFakeStore(), &fakeSender{}, nil)
	if _, err := s.Signup(context.Background(), "not-an-email", "password123", ""); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("err = %v; want ErrInvalidEmail", err)
	}
	if _, err := s.Signup(context.Background(), "a@b.com", "short", ""); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("err = %v; want ErrWeakPassword", err)
	}
}

func TestLoginMultiOrgSelection(t *testing.T) {
	s := newTestService(t, newFakeStore(), &fakeSender{}, nil)
	ctx := context.Background()
	// Same email + password in two workspaces (Slack model).
	if _, err := s.Signup(ctx, "a@b.com", "password123", "Org One"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Signup(ctx, "a@b.com", "password123", "Org Two"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Login(ctx, "a@b.com", "password123", "")
	var sel *OrgSelectionError
	if !errors.As(err, &sel) || len(sel.Options) != 2 {
		t.Fatalf("err = %v; want OrgSelectionError with 2 options", err)
	}
	if _, err := s.Login(ctx, "a@b.com", "password123", sel.Options[0].OrgID); err != nil {
		t.Fatalf("login with org_id: %v", err)
	}
	if _, err := s.Login(ctx, "a@b.com", "password123", "not-a-real-org"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v; want ErrInvalidCredentials for wrong org_id", err)
	}
}

func TestLoginWrongPasswordAndUnknownEmail(t *testing.T) {
	s := newTestService(t, newFakeStore(), &fakeSender{}, nil)
	ctx := context.Background()
	if _, err := s.Signup(ctx, "a@b.com", "password123", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "a@b.com", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v; want ErrInvalidCredentials", err)
	}
	if _, err := s.Login(ctx, "nobody@b.com", "password123", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v; want ErrInvalidCredentials", err)
	}
}

func TestFullOTPFlowUpgradesToFullSession(t *testing.T) {
	store, sender := newFakeStore(), &fakeSender{}
	s := newTestService(t, store, sender, nil)
	ctx := context.Background()

	sess, err := s.Signup(ctx, "a@b.com", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := auth.Verify("test-signing-key", sess.AccessToken)

	if err := s.SendOTP(ctx, claims, "+91 98765-43210"); err != nil {
		t.Fatalf("SendOTP: %v", err)
	}
	if sender.lastPhone != "+919876543210" {
		t.Fatalf("phone sent = %q; want normalized E.164", sender.lastPhone)
	}
	full, err := s.VerifyOTP(ctx, claims, sender.lastCode)
	if err != nil {
		t.Fatalf("VerifyOTP: %v", err)
	}
	if full.Scope != auth.ScopeFull || full.RefreshToken == "" || !full.PhoneVerified {
		t.Fatalf("want full session with refresh token, got %+v", full)
	}
	// Next login goes straight to a full pair.
	again, err := s.Login(ctx, "a@b.com", "password123", "")
	if err != nil || again.Scope != auth.ScopeFull || again.RefreshToken == "" {
		t.Fatalf("re-login = %+v, %v; want full pair", again, err)
	}
}

func TestSendOTPRejectsBadPhoneAndVerified(t *testing.T) {
	store := newFakeStore()
	s := newTestService(t, store, &fakeSender{}, nil)
	ctx := context.Background()
	sess, _ := s.Signup(ctx, "a@b.com", "password123", "")
	claims, _ := auth.Verify("test-signing-key", sess.AccessToken)

	if err := s.SendOTP(ctx, claims, "9876543210"); !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("err = %v; want ErrInvalidPhone (missing +country)", err)
	}
	if err := s.SendOTP(ctx, claims, "+919876543210"); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	if err := s.SendOTP(ctx, claims, "+919876543210"); !errors.Is(err, ErrOTPCooldown) {
		t.Fatalf("err = %v; want ErrOTPCooldown", err)
	}
	_ = sender
	if err := store.SetPhoneVerified(ctx, claims.UserID, "+911111111111"); err != nil {
		t.Fatal(err)
	}
	if err := s.SendOTP(ctx, claims, "+919876543210"); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("err = %v; want ErrAlreadyVerified", err)
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	store := newFakeStore()
	s := newTestService(t, store, &fakeSender{}, nil)
	ctx := context.Background()
	sess, _ := s.Signup(ctx, "a@b.com", "password123", "")
	claims, _ := auth.Verify("test-signing-key", sess.AccessToken)
	_ = s.SendOTP(ctx, claims, "+919876543210")
	full, err := s.VerifyOTP(ctx, claims, s.mustLastCode(t))
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := s.Refresh(ctx, full.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if rotated.RefreshToken == full.RefreshToken {
		t.Fatal("refresh token must rotate")
	}
	// Replaying the spent token is reuse → rejected and chain revoked.
	if _, err := s.Refresh(ctx, full.RefreshToken); !errors.Is(err, ErrTokenReuse) {
		t.Fatalf("err = %v; want ErrTokenReuse", err)
	}
	if _, err := s.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, ErrTokenReuse) {
		t.Fatalf("descendant after reuse err = %v; want ErrTokenReuse (chain revoked)", err)
	}
}

func TestRefreshRejectsAccessToken(t *testing.T) {
	s := newTestService(t, newFakeStore(), &fakeSender{}, nil)
	access, _ := auth.Issue("test-signing-key", auth.Claims{UserID: "u", OrgID: "o", Scope: auth.ScopeFull}, time.Minute)
	if _, err := s.Refresh(context.Background(), access); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v; want ErrInvalidToken (access token is not a refresh token)", err)
	}
}

func TestGoogleAuthPaths(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()

	// New Google user → new org, pre session.
	s := newTestService(t, store, &fakeSender{}, fakeGoogle{GoogleIdentity{Sub: "g-1", Email: "g@x.com", EmailVerified: true}})
	sess, err := s.GoogleAuth(ctx, "raw-token", "")
	if err != nil || sess.Scope != auth.ScopePre {
		t.Fatalf("google signup = %+v, %v; want pre session", sess, err)
	}
	// Same sub again → login, same single user.
	if _, err := s.GoogleAuth(ctx, "raw-token", ""); err != nil {
		t.Fatalf("google login: %v", err)
	}
	if len(store.users) != 1 {
		t.Fatalf("users = %d; want 1 (login, not second signup)", len(store.users))
	}

	// Manual account with same email → Google gets linked, no new org.
	if _, err := s.Signup(ctx, "manual@x.com", "password123", ""); err != nil {
		t.Fatal(err)
	}
	s2 := newTestService(t, store, &fakeSender{}, fakeGoogle{GoogleIdentity{Sub: "g-2", Email: "manual@x.com", EmailVerified: true}})
	if _, err := s2.GoogleAuth(ctx, "raw-token", ""); err != nil {
		t.Fatalf("google link: %v", err)
	}
	linked, _ := store.FindAllByEmail(ctx, "manual@x.com")
	if len(linked) != 1 || linked[0].GoogleSub != "g-2" {
		t.Fatalf("google sub not linked: %+v", linked)
	}
	if len(store.users) != 2 {
		t.Fatalf("users = %d; want 2 (linked, not created)", len(store.users))
	}

	// Email in two orgs → Google can't pick → ErrEmailAmbiguous.
	if _, err := s.Signup(ctx, "manual@x.com", "password123", "Second Org"); err != nil {
		t.Fatal(err)
	}
	s3 := newTestService(t, store, &fakeSender{}, fakeGoogle{GoogleIdentity{Sub: "g-3", Email: "manual@x.com", EmailVerified: true}})
	if _, err := s3.GoogleAuth(ctx, "raw-token", ""); !errors.Is(err, ErrEmailAmbiguous) {
		t.Fatalf("err = %v; want ErrEmailAmbiguous", err)
	}
}

// mustLastCode digs the last issued code out via the dev-visible sender.
func (s *Service) mustLastCode(t *testing.T) string {
	t.Helper()
	fs, ok := s.sender.(*fakeSender)
	if !ok || fs.lastCode == "" {
		t.Fatal("no code captured by fake sender")
	}
	return fs.lastCode
}

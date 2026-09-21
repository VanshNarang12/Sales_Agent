package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestOTP(t *testing.T) (*OTPManager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewOTPManager(rdb, OTPConfig{
		TTL:            5 * time.Minute,
		MaxAttempts:    3,
		ResendCooldown: time.Minute,
		DailyCap:       3,
	}), mr
}

func TestOTPIssueAndVerify(t *testing.T) {
	m, _ := newTestOTP(t)
	ctx := context.Background()
	code, err := m.Issue(ctx, "u1", "+919876543210")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q: want 6 digits", code)
	}
	phone, err := m.Verify(ctx, "u1", code)
	if err != nil || phone != "+919876543210" {
		t.Fatalf("Verify = %q, %v; want phone back, nil", phone, err)
	}
	// State is cleared after success: same code no longer works.
	if _, err := m.Verify(ctx, "u1", code); !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("second Verify err = %v; want ErrOTPExpired", err)
	}
}

func TestOTPWrongCodeAndMaxAttempts(t *testing.T) {
	m, _ := newTestOTP(t)
	ctx := context.Background()
	code, _ := m.Issue(ctx, "u1", "+919876543210")
	for i := 0; i < 3; i++ {
		if _, err := m.Verify(ctx, "u1", "000000"); !errors.Is(err, ErrOTPMismatch) {
			t.Fatalf("attempt %d err = %v; want ErrOTPMismatch", i+1, err)
		}
	}
	// Attempt 4 exceeds MaxAttempts=3 and wipes the code — even the right one fails.
	if _, err := m.Verify(ctx, "u1", code); !errors.Is(err, ErrOTPMaxAttempts) {
		t.Fatalf("err = %v; want ErrOTPMaxAttempts", err)
	}
	if _, err := m.Verify(ctx, "u1", code); !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("after wipe err = %v; want ErrOTPExpired", err)
	}
}

func TestOTPResendCooldown(t *testing.T) {
	m, mr := newTestOTP(t)
	ctx := context.Background()
	if _, err := m.Issue(ctx, "u1", "+919876543210"); err != nil {
		t.Fatalf("first Issue: %v", err)
	}
	if _, err := m.Issue(ctx, "u1", "+919876543210"); !errors.Is(err, ErrOTPCooldown) {
		t.Fatalf("err = %v; want ErrOTPCooldown", err)
	}
	mr.FastForward(time.Minute + time.Second)
	if _, err := m.Issue(ctx, "u1", "+919876543210"); err != nil {
		t.Fatalf("Issue after cooldown: %v", err)
	}
}

func TestOTPDailyCap(t *testing.T) {
	m, mr := newTestOTP(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := m.Issue(ctx, "u1", "+919876543210"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
		mr.FastForward(time.Minute + time.Second)
	}
	if _, err := m.Issue(ctx, "u1", "+919876543210"); !errors.Is(err, ErrOTPDailyCap) {
		t.Fatalf("err = %v; want ErrOTPDailyCap", err)
	}
}

func TestOTPExpires(t *testing.T) {
	m, mr := newTestOTP(t)
	ctx := context.Background()
	code, _ := m.Issue(ctx, "u1", "+919876543210")
	mr.FastForward(6 * time.Minute)
	if _, err := m.Verify(ctx, "u1", code); !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("err = %v; want ErrOTPExpired", err)
	}
}

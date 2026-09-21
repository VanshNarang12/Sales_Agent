package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrOTPCooldown    = errors.New("identity: resend cooldown active")
	ErrOTPDailyCap    = errors.New("identity: daily OTP send cap reached")
	ErrOTPExpired     = errors.New("identity: no pending OTP (expired or never sent)")
	ErrOTPMismatch    = errors.New("identity: wrong code")
	ErrOTPMaxAttempts = errors.New("identity: too many wrong attempts")
)

// OTPConfig tunes the Redis OTP manager (auth_techdoc.md §9).
type OTPConfig struct {
	TTL            time.Duration
	MaxAttempts    int
	ResendCooldown time.Duration
	DailyCap       int
}

// OTPManager issues and verifies one-time codes. Only a SHA-256 of the code and the
// pending phone are stored, all keys carry a TTL — nothing survives the flow.
type OTPManager struct {
	rdb *redis.Client
	cfg OTPConfig
}

func NewOTPManager(rdb *redis.Client, cfg OTPConfig) *OTPManager {
	return &OTPManager{rdb: rdb, cfg: cfg}
}

func otpKey(userID, part string) string { return "otp:" + part + ":" + userID }

// Issue generates a 6-digit code for the user and records the pending phone.
func (m *OTPManager) Issue(ctx context.Context, userID, phone string) (string, error) {
	ok, err := m.rdb.SetNX(ctx, otpKey(userID, "cooldown"), "1", m.cfg.ResendCooldown).Result()
	if err != nil {
		return "", fmt.Errorf("otp cooldown: %w", err)
	}
	if !ok {
		return "", ErrOTPCooldown
	}
	dayKey := otpKey(userID, "sends") + ":" + time.Now().UTC().Format("2006-01-02")
	sends, err := m.rdb.Incr(ctx, dayKey).Result()
	if err != nil {
		return "", fmt.Errorf("otp daily cap: %w", err)
	}
	m.rdb.Expire(ctx, dayKey, 24*time.Hour)
	if int(sends) > m.cfg.DailyCap {
		return "", ErrOTPDailyCap
	}
	code, err := randomCode()
	if err != nil {
		return "", err
	}
	pipe := m.rdb.TxPipeline()
	pipe.Set(ctx, otpKey(userID, "code"), hashCode(code), m.cfg.TTL)
	pipe.Set(ctx, otpKey(userID, "phone"), phone, m.cfg.TTL)
	pipe.Del(ctx, otpKey(userID, "attempts"))
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("otp store: %w", err)
	}
	return code, nil
}

// Verify checks the code and, on success, returns the pending phone and clears state.
func (m *OTPManager) Verify(ctx context.Context, userID, code string) (string, error) {
	want, err := m.rdb.Get(ctx, otpKey(userID, "code")).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrOTPExpired
	}
	if err != nil {
		return "", fmt.Errorf("otp read: %w", err)
	}
	attempts, err := m.rdb.Incr(ctx, otpKey(userID, "attempts")).Result()
	if err != nil {
		return "", fmt.Errorf("otp attempts: %w", err)
	}
	m.rdb.Expire(ctx, otpKey(userID, "attempts"), m.cfg.TTL)
	if int(attempts) > m.cfg.MaxAttempts {
		m.clear(ctx, userID)
		return "", ErrOTPMaxAttempts
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(want)) != 1 {
		return "", ErrOTPMismatch
	}
	phone, err := m.rdb.Get(ctx, otpKey(userID, "phone")).Result()
	if err != nil {
		return "", ErrOTPExpired
	}
	m.clear(ctx, userID)
	return phone, nil
}

func (m *OTPManager) clear(ctx context.Context, userID string) {
	m.rdb.Del(ctx, otpKey(userID, "code"), otpKey(userID, "phone"), otpKey(userID, "attempts"))
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("otp rand: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

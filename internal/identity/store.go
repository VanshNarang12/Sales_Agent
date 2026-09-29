package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

var (
	ErrEmailTaken   = errors.New("identity: email already registered")
	ErrPhoneTaken   = errors.New("identity: phone already registered")
	ErrUserNotFound = errors.New("identity: user not found")
	ErrTokenReuse   = errors.New("identity: refresh token reused, revoked, or expired")
)

// User is the identity view of a users row. OrgName is filled by login lookups
// only (it feeds the multi-org picker).
type User struct {
	ID              string
	OrgID           string
	OrgName         string
	Email           string
	Role            string // "admin" (org creator) or "member" (added via invite, Stage 18)
	PasswordHash    string
	GoogleSub       string
	Phone           string
	PhoneVerifiedAt *time.Time
}

// Store persists orgs, users, and refresh tokens. Tenant-scoped operations go
// through db.WithTenantTx; login lookups use the 0005 login-lookup RLS policies.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const userCols = "id, org_id, email, role, COALESCE(password_hash,''), COALESCE(google_sub,''), COALESCE(phone,''), phone_verified_at"

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.Role, &u.PasswordHash, &u.GoogleSub, &u.Phone, &u.PhoneVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// CreateOrgWithUser creates the tenant and its first user in one transaction. The
// org id is generated app-side so RLS (app.tenant_id) admits the inserts.
func (s *Store) CreateOrgWithUser(ctx context.Context, orgName, email, passwordHash, googleSub, provider string) (User, error) {
	orgID, userID := uuid.NewString(), uuid.NewString()
	email = strings.ToLower(strings.TrimSpace(email))
	ctx = tenancy.With(ctx, tenancy.TenantID(orgID))
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO orgs (id, name) VALUES ($1, $2)", orgID, orgName); err != nil {
			return fmt.Errorf("insert org: %w", err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO users (id, org_id, email, role, password_hash, google_sub, oauth_provider)
			 VALUES ($1, $2, $3, 'admin', NULLIF($4,''), NULLIF($5,''), NULLIF($6,''))`,
			userID, orgID, email, passwordHash, googleSub, provider)
		return err
	})
	if err != nil {
		return User{}, fmt.Errorf("create org+user: %w", err)
	}
	return User{ID: userID, OrgID: orgID, OrgName: orgName, Email: email, Role: "admin", PasswordHash: passwordHash, GoogleSub: googleSub}, nil
}

// FindAllByEmail returns every account holding the email, across orgs, with org
// names (login-lookup + org_login_lookup policies). Same email in different orgs
// is allowed — the caller disambiguates.
func (s *Store) FindAllByEmail(ctx context.Context, email string) ([]User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin login tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.login_email', $1, true)", email); err != nil {
		return nil, fmt.Errorf("set login guc: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.org_id, COALESCE(o.name,''), u.email, u.role, COALESCE(u.password_hash,''),
		       COALESCE(u.google_sub,''), COALESCE(u.phone,''), u.phone_verified_at
		FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		WHERE lower(u.email) = $1
		ORDER BY u.created_at`, email)
	if err != nil {
		return nil, fmt.Errorf("login lookup: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OrgID, &u.OrgName, &u.Email, &u.Role, &u.PasswordHash, &u.GoogleSub, &u.Phone, &u.PhoneVerifiedAt); err != nil {
			return nil, fmt.Errorf("scan login user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// FindByGoogleSub looks a user up by Google subject (login-lookup policy). The sub
// is globally unique, so this is always at most one row.
func (s *Store) FindByGoogleSub(ctx context.Context, sub string) (User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin login tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.login_sub', $1, true)", sub); err != nil {
		return User{}, fmt.Errorf("set login guc: %w", err)
	}
	u, err := scanUser(tx.QueryRow(ctx, "SELECT "+userCols+" FROM users WHERE google_sub = $1", sub))
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}

// GetUser reads a user inside the current tenant.
func (s *Store) GetUser(ctx context.Context, userID string) (User, error) {
	var u User
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, "SELECT "+userCols+" FROM users WHERE id = $1", userID))
		return err
	})
	return u, err
}

// LinkGoogle attaches a Google subject to an existing (email-matched) user.
func (s *Store) LinkGoogle(ctx context.Context, userID, sub string) error {
	return db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE users SET google_sub = $1, oauth_provider = 'google' WHERE id = $2", sub, userID)
		return err
	})
}

// SetPhoneVerified stamps the verified phone; the global unique index is the
// anti-abuse gate (16.13) — a taken phone maps to ErrPhoneTaken.
func (s *Store) SetPhoneVerified(ctx context.Context, userID, phone string) error {
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE users SET phone = $1, phone_verified_at = now() WHERE id = $2", phone, userID)
		return err
	})
	if isUnique(err, "users_phone_unique") {
		return ErrPhoneTaken
	}
	return err
}

// InsertRefresh records a newly issued refresh token's lifecycle row.
func (s *Store) InsertRefresh(ctx context.Context, jti, orgID, userID string, expiresAt time.Time) error {
	return db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO refresh_tokens (id, org_id, user_id, expires_at) VALUES ($1, $2, $3, $4)",
			jti, orgID, userID, expiresAt)
		return err
	})
}

// RotateRefresh exchanges oldJTI for newJTI atomically. A spent (rotated/revoked)
// token being replayed revokes its whole descendant chain and returns ErrTokenReuse.
func (s *Store) RotateRefresh(ctx context.Context, oldJTI, newJTI string, newExpiry time.Time) error {
	return db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		var userID, orgID string
		var expiresAt time.Time
		var rotatedAt, revokedAt *time.Time
		err := tx.QueryRow(ctx,
			"SELECT org_id, user_id, expires_at, rotated_at, revoked_at FROM refresh_tokens WHERE id = $1 FOR UPDATE",
			oldJTI).Scan(&orgID, &userID, &expiresAt, &rotatedAt, &revokedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenReuse
		}
		if err != nil {
			return fmt.Errorf("load refresh: %w", err)
		}
		if rotatedAt != nil || revokedAt != nil {
			if _, err := tx.Exec(ctx, `
				WITH RECURSIVE chain AS (
					SELECT id, replaced_by FROM refresh_tokens WHERE id = $1
					UNION ALL
					SELECT rt.id, rt.replaced_by FROM refresh_tokens rt JOIN chain c ON rt.id = c.replaced_by
				)
				UPDATE refresh_tokens SET revoked_at = now()
				WHERE id IN (SELECT id FROM chain) AND revoked_at IS NULL`, oldJTI); err != nil {
				return fmt.Errorf("revoke chain: %w", err)
			}
			return ErrTokenReuse
		}
		if time.Now().After(expiresAt) {
			return ErrTokenReuse
		}
		if _, err := tx.Exec(ctx,
			"UPDATE refresh_tokens SET rotated_at = now(), replaced_by = $1 WHERE id = $2", newJTI, oldJTI); err != nil {
			return fmt.Errorf("rotate refresh: %w", err)
		}
		_, err = tx.Exec(ctx,
			"INSERT INTO refresh_tokens (id, org_id, user_id, expires_at) VALUES ($1, $2, $3, $4)",
			newJTI, orgID, userID, newExpiry)
		return err
	})
}

// RevokeRefresh marks one token revoked (logout). Idempotent.
func (s *Store) RevokeRefresh(ctx context.Context, jti string) error {
	return db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL", jti)
		return err
	})
}

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

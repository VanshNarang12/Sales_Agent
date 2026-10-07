// Package playbook stores the org's live-guidance levers (Stage 11): a guidance
// text and do-not-say rules, compiled into one prompt block for card generation.
// Enforcement is prompt-only — no post-generation check (user decision 2026-10-03,
// admin_playbook_techdoc.md §3).
package playbook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

var ErrRuleNotFound = errors.New("playbook: rule not found")

type Rule struct {
	ID        string    `json:"id"`
	Phrase    string    `json:"phrase"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type Playbook struct {
	Guidance string `json:"guidance"`
	Rules    []Rule `json:"rules"`
}

// The cache is shared Redis (user decision 2026-10-06), so one admin write
// refreshes what every gateway instance reads. The uncached DB read is a full
// tenant tx against a possibly-remote DB (~5 round trips, measured ~1.9 s on
// dev), so the Suggest hot path must only ever pay a Redis GET:
//   - soft-stale entries (older than softTTL) are served instantly while a
//     single-flight background goroutine re-fetches (lock = Redis SETNX);
//   - writes refetch from the DB and overwrite the entry, so edits are visible
//     to all instances immediately and the next reader pays no miss;
//   - hardTTL is the self-heal backstop if a write-side refresh is ever lost.
const (
	softTTL    = 10 * time.Minute
	hardTTL    = 24 * time.Hour
	refreshTTL = 30 * time.Second // single-flight lock lifetime
)

// Cache is the minimal Redis surface the store needs; narrow so tests can fake it.
type Cache interface {
	Get(ctx context.Context, key string) (val string, ok bool, err error)
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

// envelope is the cached value: the playbook plus when it was read from the DB.
type envelope struct {
	FetchedAt time.Time `json:"fetched_at"`
	Playbook  Playbook  `json:"playbook"`
}

func cacheKey(tid string) string   { return "playbook:" + tid }
func refreshKey(tid string) string { return "playbook_refresh:" + tid }

type Store struct {
	pool  *pgxpool.Pool
	cache Cache // nil ⇒ every read hits the DB (Redis down at boot)
}

func NewStore(pool *pgxpool.Pool, cache Cache) *Store {
	return &Store{pool: pool, cache: cache}
}

// Get returns the org's playbook; a never-saved playbook is empty, not an error.
// Served from the shared cache — soft-stale entries are returned as-is with a
// background refresh — so only a tenant's first-ever read blocks on the DB.
// Callers must not mutate the result.
func (s *Store) Get(ctx context.Context) (Playbook, error) {
	tid, err := tenancy.MustFrom(ctx)
	if err != nil {
		return Playbook{}, err
	}
	if s.cache != nil {
		if raw, ok, err := s.cache.Get(ctx, cacheKey(string(tid))); err == nil && ok {
			var e envelope
			if json.Unmarshal([]byte(raw), &e) == nil {
				if time.Since(e.FetchedAt) > softTTL {
					s.refreshAsync(string(tid))
				}
				return e.Playbook, nil
			}
		}
	}
	p, err := s.fetch(ctx)
	if err != nil {
		return Playbook{}, err
	}
	s.put(ctx, string(tid), p)
	return p, nil
}

// put stores the playbook in the shared cache; failures are dropped — the next
// reader just fetches again.
func (s *Store) put(ctx context.Context, tid string, pb Playbook) {
	if s.cache == nil {
		return
	}
	raw, err := json.Marshal(envelope{FetchedAt: time.Now(), Playbook: pb})
	if err != nil {
		return
	}
	_ = s.cache.Set(ctx, cacheKey(tid), string(raw), hardTTL)
}

// refreshAsync re-fetches one tenant's playbook off the hot path. The SETNX lock
// makes the refresh single-flight across every gateway instance; errors are
// dropped — the stale entry stays until hardTTL.
func (s *Store) refreshAsync(tid string) {
	go func() {
		ctx, cancel := context.WithTimeout(tenancy.With(context.Background(), tenancy.TenantID(tid)), 15*time.Second)
		defer cancel()
		got, err := s.cache.SetNX(ctx, refreshKey(tid), "1", refreshTTL)
		if err != nil || !got {
			return
		}
		if p, err := s.fetch(ctx); err == nil {
			s.put(ctx, tid, p)
		}
		_ = s.cache.Del(ctx, refreshKey(tid))
	}()
}

// refreshOnWrite runs after a successful write: re-read the DB (the only code
// path that builds a playbook — the cache can never diverge from it) and
// overwrite the shared entry so every instance sees the edit immediately. If
// the re-read fails, drop the entry so the next reader fetches fresh.
func (s *Store) refreshOnWrite(ctx context.Context) {
	tid, err := tenancy.MustFrom(ctx)
	if err != nil || s.cache == nil {
		return
	}
	if p, err := s.fetch(ctx); err == nil {
		s.put(ctx, string(tid), p)
		return
	}
	_ = s.cache.Del(ctx, cacheKey(string(tid)))
}

// fetch is the uncached read: one tenant tx, both tables.
func (s *Store) fetch(ctx context.Context) (Playbook, error) {
	p := Playbook{Rules: []Rule{}}
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT guidance FROM playbooks WHERE org_id = current_setting('app.tenant_id')::uuid`).
			Scan(&p.Guidance)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		rows, err := tx.Query(ctx,
			`SELECT id, phrase, reason, created_at FROM playbook_rules ORDER BY created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Rule
			if err := rows.Scan(&r.ID, &r.Phrase, &r.Reason, &r.CreatedAt); err != nil {
				return err
			}
			p.Rules = append(p.Rules, r)
		}
		return rows.Err()
	})
	if err != nil {
		return Playbook{}, fmt.Errorf("playbook get: %w", err)
	}
	return p, nil
}

// SetGuidance upserts the org's guidance text.
func (s *Store) SetGuidance(ctx context.Context, guidance, updatedBy string) error {
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO playbooks (org_id, guidance, updated_by, updated_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, NULLIF($2,'')::uuid, now())
			ON CONFLICT (org_id) DO UPDATE
			SET guidance = $1, updated_by = NULLIF($2,'')::uuid, updated_at = now()`,
			guidance, updatedBy)
		return err
	})
	if err != nil {
		return fmt.Errorf("playbook set guidance: %w", err)
	}
	s.refreshOnWrite(ctx)
	return nil
}

// AddRule inserts one do-not-say rule and returns it.
func (s *Store) AddRule(ctx context.Context, phrase, reason, createdBy string) (Rule, error) {
	r := Rule{Phrase: strings.TrimSpace(phrase), Reason: strings.TrimSpace(reason)}
	if r.Phrase == "" {
		return Rule{}, fmt.Errorf("playbook add rule: empty phrase")
	}
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO playbook_rules (org_id, phrase, reason, created_by)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, NULLIF($3,'')::uuid)
			RETURNING id, created_at`,
			r.Phrase, r.Reason, createdBy).Scan(&r.ID, &r.CreatedAt)
	})
	if err != nil {
		return Rule{}, fmt.Errorf("playbook add rule: %w", err)
	}
	s.refreshOnWrite(ctx)
	return r, nil
}

// DeleteRule removes one rule.
func (s *Store) DeleteRule(ctx context.Context, id string) error {
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM playbook_rules WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrRuleNotFound
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrRuleNotFound) {
		return fmt.Errorf("playbook delete rule: %w", err)
	}
	if err == nil {
		s.refreshOnWrite(ctx)
	}
	return err
}

// PromptBlock compiles the playbook into the text injected into the card LLM's
// system prompt (admin_playbook_techdoc.md §8). Empty playbook ⇒ "".
func (p Playbook) PromptBlock() string {
	guidance := strings.TrimSpace(p.Guidance)
	if guidance == "" && len(p.Rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Org playbook — follow strictly:\n")
	if guidance != "" {
		b.WriteString(guidance)
		b.WriteString("\n")
	}
	if len(p.Rules) > 0 {
		b.WriteString("You must NEVER say, promise, or imply any of the following:\n")
		for _, r := range p.Rules {
			if r.Reason != "" {
				fmt.Fprintf(&b, "- %q (%s)\n", r.Phrase, r.Reason)
			} else {
				fmt.Fprintf(&b, "- %q\n", r.Phrase)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

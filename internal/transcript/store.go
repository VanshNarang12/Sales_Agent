package transcript

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Entry is one final utterance of the call (times are call-relative ms).
type Entry struct {
	Speaker string
	Text    string
	StartMs int64
	EndMs   int64
}

type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Store {
	return &Store{rdb: rdb, ttl: ttl}
}

// Tenant is part of the key: isolation holds even in a shared Redis (standards §4).
func key(tenantID, sessionID string) string {
	return "transcript:" + tenantID + ":" + sessionID
}

// Append writes one utterance and refreshes the sliding TTL in a single round-trip.
func (s *Store) Append(ctx context.Context, tenantID, sessionID string, e Entry) error {
	k := key(tenantID, sessionID)
	// startMs prefix keeps members unique when two utterances share an endMs score.
	member := fmt.Sprintf("%d|%s: %s", e.StartMs, e.Speaker, e.Text)
	pipe := s.rdb.Pipeline()
	pipe.ZAdd(ctx, k, redis.Z{Score: float64(e.EndMs), Member: member})
	pipe.Expire(ctx, k, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("transcript append: %w", err)
	}
	return nil
}

// Window returns the utterances from the last lookbackMs (relative to the newest
// utterance), oldest first. An absent or empty session yields (nil, nil).
func (s *Store) Window(ctx context.Context, tenantID, sessionID string, lookbackMs int64) ([]Entry, error) {
	k := key(tenantID, sessionID)
	newest, err := s.rdb.ZRevRangeWithScores(ctx, k, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("transcript window: %w", err)
	}
	if len(newest) == 0 {
		return nil, nil
	}
	cutoff := int64(newest[0].Score) - lookbackMs

	vals, err := s.rdb.ZRangeByScoreWithScores(ctx, k, &redis.ZRangeBy{
		Min: strconv.FormatInt(cutoff, 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("transcript window: %w", err)
	}
	out := make([]Entry, 0, len(vals))
	for _, z := range vals {
		m, ok := z.Member.(string)
		if !ok {
			continue
		}
		if e, ok := parseMember(m); ok {
			e.EndMs = int64(z.Score)
			out = append(out, e)
		}
	}
	return out, nil
}

// parseMember decodes "{startMs}|{speaker}: {text}".
func parseMember(m string) (Entry, bool) {
	sep := strings.IndexByte(m, '|')
	if sep < 1 {
		return Entry{}, false
	}
	startMs, err := strconv.ParseInt(m[:sep], 10, 64)
	if err != nil {
		return Entry{}, false
	}
	rest := m[sep+1:]
	colon := strings.Index(rest, ": ")
	if colon < 1 {
		return Entry{}, false
	}
	return Entry{
		Speaker: rest[:colon],
		Text:    rest[colon+2:],
		StartMs: startMs,
	}, true
}

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Service-token scopes (LYCM-140). A service token is a credential for a
// program, not a person, and carries exactly one of these.
const (
	// ScopeEidolonRead authorizes the read-only Project Eidolon hooks.
	ScopeEidolonRead = "eidolon:read"
	// ScopeDeliverySend authorizes triggering a "Send to Kindle" delivery.
	ScopeDeliverySend = "delivery:send"
	// ScopeAdminRead authorizes GET /admin/users/active, a count and nothing else.
	ScopeAdminRead = "admin:read"
)

// ErrUnknownScope is returned when minting with a scope Lyceum does not define,
// so a typo fails at the CLI instead of minting a token no route accepts.
var ErrUnknownScope = errors.New("store: unknown service-token scope")

// ValidScopes lists every scope a service token may carry.
func ValidScopes() []string {
	return []string{ScopeEidolonRead, ScopeDeliverySend, ScopeAdminRead}
}

func validScope(s string) bool {
	for _, v := range ValidScopes() {
		if s == v {
			return true
		}
	}
	return false
}

// serviceTokenPrefix marks a service token so it is never mistaken for a lyc_
// user session in a log or a paste.
const serviceTokenPrefix = "lys_"

// Audit actions and target types written by this file.
const (
	auditActionTokenMint    = "service_token.mint"
	auditActionTokenRevoke  = "service_token.revoke"
	auditActionTokenRefused = "service_token.refused"
	auditTargetServiceToken = "service_token"
	auditTargetTokenHash    = "token_hash_prefix"

	// refusedPrefixLen is how much of the SHA-256 identifies a refused
	// presentation. Enough to correlate repeats, far too little to look up.
	refusedPrefixLen = 12
)

// ServiceToken is a service_tokens row as the CLI lists it. It carries no hash:
// nothing outside this package ever needs one.
type ServiceToken struct {
	ID            int64
	ConsumerLabel string
	Scope         string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

// MintServiceToken issues a service token for a consumer and returns its
// plaintext, which is not recoverable afterwards. The insert and its audit row
// share a transaction, and the audit write survives a cancelled ctx for the
// same reason as revoke: the trail must not lose a change that happened.
func (s *Store) MintServiceToken(ctx context.Context, label, scope, actor string) (int64, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0, "", errors.New("store: service-token label is required")
	}
	if !validScope(scope) {
		return 0, "", fmt.Errorf("%w %q (valid: %s)", ErrUnknownScope, scope, strings.Join(ValidScopes(), ", "))
	}
	plaintext, err := newTokenWithPrefix(serviceTokenPrefix)
	if err != nil {
		return 0, "", err
	}

	ctx = context.WithoutCancel(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("store: mint service token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO service_tokens (consumer_label, scope, token_hash)
		 VALUES ($1, $2, $3) RETURNING id`,
		label, scope, hashToken(plaintext)).Scan(&id); err != nil {
		return 0, "", fmt.Errorf("store: mint service token: %w", err)
	}
	if err := writeAudit(ctx, tx, actor, auditActionTokenMint, auditTargetServiceToken, fmt.Sprint(id),
		map[string]any{"label": label, "scope": scope}); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, "", fmt.Errorf("store: mint service token: %w", err)
	}
	return id, plaintext, nil
}

// RevokeServiceTokenByID revokes one live token. ErrNotFound when no such id
// exists or it is already revoked, so a typo is not reported as success.
func (s *Store) RevokeServiceTokenByID(ctx context.Context, id int64, actor string) error {
	n, err := s.revokeServiceTokens(ctx, actor, "id = $1", id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeServiceTokensByLabel revokes every live token carrying a label and
// returns how many. Zero is ErrNotFound for the same reason as by id.
func (s *Store) RevokeServiceTokensByLabel(ctx context.Context, label, actor string) (int, error) {
	n, err := s.revokeServiceTokens(ctx, actor, "consumer_label = $1", label)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	return n, nil
}

// revokeServiceTokens sets revoked_at on every live row matching where (one
// argument) and writes one audit row per token, all in one transaction.
func (s *Store) revokeServiceTokens(ctx context.Context, actor, where string, arg any) (int, error) {
	ctx = context.WithoutCancel(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: revoke service token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	rows, err := tx.Query(ctx,
		`UPDATE service_tokens SET revoked_at = now()
		  WHERE revoked_at IS NULL AND `+where+`
		  RETURNING id, consumer_label, scope`, arg)
	if err != nil {
		return 0, fmt.Errorf("store: revoke service token: %w", err)
	}
	type revoked struct {
		id           int64
		label, scope string
	}
	var done []revoked
	for rows.Next() {
		var r revoked
		if err := rows.Scan(&r.id, &r.label, &r.scope); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: revoke service token: %w", err)
		}
		done = append(done, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: revoke service token: %w", err)
	}
	for _, r := range done {
		if err := writeAudit(ctx, tx, actor, auditActionTokenRevoke, auditTargetServiceToken, fmt.Sprint(r.id),
			map[string]any{"label": r.label, "scope": r.scope}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: revoke service token: %w", err)
	}
	return len(done), nil
}

// ListServiceTokens returns every token, live and revoked, oldest first.
func (s *Store) ListServiceTokens(ctx context.Context) ([]ServiceToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, consumer_label, scope, created_at, last_used_at, revoked_at
		   FROM service_tokens ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list service tokens: %w", err)
	}
	defer rows.Close()
	var out []ServiceToken
	for rows.Next() {
		var t ServiceToken
		if err := rows.Scan(&t.ID, &t.ConsumerLabel, &t.Scope, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("store: scan service token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ServiceTokenCheck is what a lookup learned about a presented token.
type ServiceTokenCheck struct {
	Label   string
	Scope   string
	Revoked bool
}

// CheckServiceToken looks a presented token up by its hash. ErrNotFound means no
// such token. A revoked token is returned with Revoked set so the caller can
// audit it by label while answering exactly as it does for an unknown one.
//
// last_used_at is stamped in the same statement, only for a live token that
// holds wantScope, and at most once a minute so a busy consumer does not turn
// every read into a write.
func (s *Store) CheckServiceToken(ctx context.Context, plaintext, wantScope string) (ServiceTokenCheck, error) {
	if plaintext == "" {
		return ServiceTokenCheck{}, ErrNotFound
	}
	var c ServiceTokenCheck
	err := s.pool.QueryRow(ctx,
		`WITH hit AS (
		     SELECT id, consumer_label, scope, revoked_at IS NOT NULL AS revoked
		       FROM service_tokens WHERE token_hash = $1
		 ), touched AS (
		     UPDATE service_tokens t SET last_used_at = now()
		       FROM hit
		      WHERE t.id = hit.id AND NOT hit.revoked AND hit.scope = $2
		        AND (t.last_used_at IS NULL OR t.last_used_at < now() - interval '1 minute')
		     RETURNING t.id
		 )
		 SELECT consumer_label, scope, revoked FROM hit`,
		hashToken(plaintext), wantScope).Scan(&c.Label, &c.Scope, &c.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceTokenCheck{}, ErrNotFound
	}
	if err != nil {
		return ServiceTokenCheck{}, fmt.Errorf("store: check service token: %w", err)
	}
	return c, nil
}

// AuditRefusedServiceToken records that an unknown or revoked token was
// presented, at most once per hash prefix per hour so a client retrying in a
// loop cannot flood the table. Only a short hash prefix is stored, never the
// plaintext; label is empty when the token is unknown.
func (s *Store) AuditRefusedServiceToken(ctx context.Context, plaintext, label, reason string) error {
	prefix := hashToken(plaintext)[:refusedPrefixLen]
	detail, err := json.Marshal(map[string]any{"reason": reason, "label": label})
	if err != nil {
		return fmt.Errorf("store: audit refused token: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: audit refused token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// The advisory lock makes the check-then-insert atomic per prefix, so two
	// concurrent refusals cannot both see "none yet".
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, auditActionTokenRefused+prefix); err != nil {
		return fmt.Errorf("store: audit refused token: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target_type, target_id, detail)
		 SELECT 'api', $1, $2, $3, $4::jsonb
		  WHERE NOT EXISTS (
		      SELECT 1 FROM audit_log
		       WHERE action = $1 AND target_id = $3 AND at > now() - interval '1 hour')`,
		auditActionTokenRefused, auditTargetTokenHash, prefix, string(detail)); err != nil {
		return fmt.Errorf("store: audit refused token: %w", err)
	}
	return tx.Commit(ctx)
}

// writeAudit appends one audit_log row inside tx.
func writeAudit(ctx context.Context, tx pgx.Tx, actor, action, targetType, targetID string, detail map[string]any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("store: audit: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target_type, target_id, detail)
		 VALUES ($1, $2, $3, $4, $5::jsonb)`,
		actor, action, targetType, targetID, string(b)); err != nil {
		return fmt.Errorf("store: audit: %w", err)
	}
	return nil
}

// CountActiveUsers counts distinct accounts with a session used within window,
// and returns the database's own "now" as the instant the count describes. The
// database clock is the one that stamped last_used_at, so app-server clock skew
// cannot move the window. Nothing about who the users are leaves this function.
func (s *Store) CountActiveUsers(ctx context.Context, window time.Duration) (int, time.Time, error) {
	var n int
	var asOf time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT user_id), now()
		   FROM user_tokens
		  WHERE kind = 'session'
		    AND (expires_at IS NULL OR expires_at > now())
		    AND last_used_at > now() - make_interval(secs => $1)`,
		window.Seconds()).Scan(&n, &asOf)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("store: count active users: %w", err)
	}
	return n, asOf, nil
}

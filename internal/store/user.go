package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateEmail is returned by CreateUser when the address is already taken.
var ErrDuplicateEmail = errors.New("store: email already registered")

// pgUniqueViolation is Postgres SQLSTATE 23505, raised when an INSERT collides
// with a unique constraint.
const pgUniqueViolation = "23505"

// Token kinds (LYCM-801). An invite is single-use and is what the owner hands
// out; redeeming it yields a session, which is the long-lived per-device
// credential the clients actually carry.
const (
	TokenInvite  = "invite"
	TokenSession = "session"
)

// tokenPrefix marks a Lyceum user token, so it is recognizable in a log or a
// paste and distinguishable from a LYCEUM_API_TOKENS service token.
const tokenPrefix = "lyc_"

// InviteTTL bounds how long an unredeemed invite stays usable. Redeeming one
// yields a durable session, so an invite left in an inbox, a chat log, or a
// terminal scrollback would otherwise be a standing way in. `lyceum mint-token`
// issues a fresh one, and the server re-prints an owner invite at boot once the
// old one lapses (CountTokens ignores expired rows).
const InviteTTL = 7 * 24 * time.Hour

// User is a person with an account on this server. Exactly one user is the
// owner: the account that existed before LYCM-801 (adopting all the reading
// history), and the only one allowed to invite or remove others.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	IsOwner     bool
	CreatedAt   time.Time
}

// Token is a credential row. The plaintext is never stored — only its SHA-256 —
// so it can be shown exactly once, at mint time.
type Token struct {
	ID         int64
	UserID     int64
	Kind       string
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	UsedAt     *time.Time
	ExpiresAt  *time.Time
}

const userColumns = `id, email, display_name, is_owner, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsOwner, &u.CreatedAt)
	return u, err
}

// normalizeEmail lowercases and trims an address so that lookups are
// case-insensitive — the CF Access JWT (LYCM-803) and Purser may not agree with
// each other on casing, and neither should be able to create a second account
// for the same person.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// hashToken returns the hex SHA-256 of a plaintext token. Lookups hash the
// presented token and query by hash, so the comparison is an index hit rather
// than a byte-by-byte compare of a secret.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// newToken generates a fresh 256-bit token, returned as a prefixed,
// URL-safe string.
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// CreateUser adds a member. The email is normalized; a collision with an
// existing account yields ErrDuplicateEmail rather than a raw constraint error.
// New users are never owners — the owner is seeded by migration 0011.
func (s *Store) CreateUser(ctx context.Context, email, displayName string) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, errors.New("store: email is required")
	}
	if displayName == "" {
		displayName = email
	}

	u, err := scanUser(s.pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name) VALUES ($1, $2)
		 RETURNING `+userColumns, email, displayName))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return User{}, ErrDuplicateEmail
	}
	if err != nil {
		return User{}, fmt.Errorf("store: create user: %w", err)
	}
	return u, nil
}

// GetUser returns the user with id, or ErrNotFound.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user: %w", err)
	}
	return u, nil
}

// GetUserByEmail returns the user with the given address (case-insensitively),
// or ErrNotFound. This is the join LYCM-803 will make against a Cloudflare
// Access-verified email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`, normalizeEmail(email)))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user by email: %w", err)
	}
	return u, nil
}

// GetOwner returns the owner account, or ErrNotFound if migration 0011 has not
// run.
func (s *Store) GetOwner(ctx context.Context) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE is_owner`))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get owner: %w", err)
	}
	return u, nil
}

// ListUsers returns every account, owner first and then by creation order.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY is_owner DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateDisplayName renames a user, returning the updated row or ErrNotFound.
func (s *Store) UpdateDisplayName(ctx context.Context, id int64, displayName string) (User, error) {
	if displayName == "" {
		return User{}, errors.New("store: display name is required")
	}
	u, err := scanUser(s.pool.QueryRow(ctx,
		`UPDATE users SET display_name = $2 WHERE id = $1 RETURNING `+userColumns,
		id, displayName))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: update display name: %w", err)
	}
	return u, nil
}

// DeleteUser removes a member and, by cascade, their tokens and reading
// positions. The owner cannot be deleted — there would be nothing to fall back
// to and no one left who can invite. Returns ErrNotFound if id is gone.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND NOT is_owner`, id)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either no such row, or it is the owner. Distinguish so the caller can
		// return 404 vs 403.
		if _, err := s.GetUser(ctx, id); err != nil {
			return err // ErrNotFound
		}
		return ErrOwnerImmutable
	}
	return nil
}

// ErrOwnerImmutable is returned when an operation would remove or demote the
// owner account.
var ErrOwnerImmutable = errors.New("store: the owner account cannot be removed")

// ReconcileOwner brings the migration-seeded owner row in line with the
// server's configured identity (LYCEUM_OWNER_EMAIL / LYCEUM_OWNER_NAME). It is
// called on every boot and is idempotent.
//
// It only ever *renames* the existing owner; it never creates a second one and
// never promotes a member. If the configured email already belongs to a
// different account it returns that account's owner unchanged together with
// ErrDuplicateEmail, so a typo'd env var is a warning the operator can fix
// rather than a server that won't boot.
func (s *Store) ReconcileOwner(ctx context.Context, email, displayName string) (User, error) {
	owner, err := s.GetOwner(ctx)
	if err != nil {
		return User{}, err
	}

	email = normalizeEmail(email)
	if email == "" || email == owner.Email {
		if displayName == "" || displayName == owner.DisplayName {
			return owner, nil
		}
		return s.UpdateDisplayName(ctx, owner.ID, displayName)
	}

	// Refuse to steal an address that is already someone else's.
	if existing, err := s.GetUserByEmail(ctx, email); err == nil && existing.ID != owner.ID {
		return owner, ErrDuplicateEmail
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return User{}, err
	}

	if displayName == "" {
		displayName = owner.DisplayName
	}
	owner, err = scanUser(s.pool.QueryRow(ctx,
		`UPDATE users SET email = $2, display_name = $3 WHERE id = $1
		 RETURNING `+userColumns, owner.ID, email, displayName))
	if err != nil {
		return User{}, fmt.Errorf("store: reconcile owner: %w", err)
	}
	return owner, nil
}

// MintToken issues a credential for a user and returns its plaintext. The
// plaintext is not recoverable afterwards: only its hash is stored, so a caller
// that loses it must mint another.
//
// expiresAt may be nil for a credential that does not expire (the normal case
// for a household session).
func (s *Store) MintToken(ctx context.Context, userID int64, kind, label string, expiresAt *time.Time) (string, error) {
	if kind != TokenInvite && kind != TokenSession {
		return "", fmt.Errorf("store: unknown token kind %q", kind)
	}
	plaintext, err := newToken()
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO user_tokens (user_id, kind, token_hash, label, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, kind, hashToken(plaintext), nullString(label), expiresAt); err != nil {
		return "", fmt.Errorf("store: mint token: %w", err)
	}
	return plaintext, nil
}

// UserByToken resolves a presented session token to its owner, or ErrNotFound
// when the token is unknown, expired, or not a session (an invite must be
// redeemed via RedeemInvite before it authenticates anything). It touches
// last_used_at as a side effect.
func (s *Store) UserByToken(ctx context.Context, plaintext string) (User, error) {
	if plaintext == "" {
		return User{}, ErrNotFound
	}
	u, err := scanUser(s.pool.QueryRow(ctx,
		`WITH touched AS (
		     UPDATE user_tokens SET last_used_at = now()
		      WHERE token_hash = $1
		        AND kind = 'session'
		        AND (expires_at IS NULL OR expires_at > now())
		     RETURNING user_id
		 )
		 SELECT `+userColumns+` FROM users WHERE id = (SELECT user_id FROM touched)`,
		hashToken(plaintext)))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: user by token: %w", err)
	}
	return u, nil
}

// RedeemInvite exchanges a single-use invite for a long-lived session token
// bound to the given device label, and returns the user together with the new
// session's plaintext.
//
// The claim and the mint happen in one transaction, and the claim is a
// conditional UPDATE on used_at, so two devices racing to redeem the same
// invite cannot both win. A spent, expired, or unknown invite yields ErrNotFound.
func (s *Store) RedeemInvite(ctx context.Context, plaintext, deviceLabel string) (User, string, error) {
	if plaintext == "" {
		return User{}, "", ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, "", fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID int64
	err = tx.QueryRow(ctx,
		`UPDATE user_tokens SET used_at = now()
		  WHERE token_hash = $1
		    AND kind = 'invite'
		    AND used_at IS NULL
		    AND (expires_at IS NULL OR expires_at > now())
		 RETURNING user_id`, hashToken(plaintext)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("store: claim invite: %w", err)
	}

	session, err := newToken()
	if err != nil {
		return User{}, "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_tokens (user_id, kind, token_hash, label)
		 VALUES ($1, 'session', $2, $3)`,
		userID, hashToken(session), nullString(deviceLabel)); err != nil {
		return User{}, "", fmt.Errorf("store: mint session: %w", err)
	}

	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, userID))
	if err != nil {
		return User{}, "", fmt.Errorf("store: load redeeming user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, "", fmt.Errorf("store: commit redeem: %w", err)
	}
	return u, session, nil
}

// RevokeToken deletes the token with the given plaintext, whoever it belongs to.
// This is how a client signs out: the credential it holds stops working
// immediately, and other devices are unaffected. Revoking an unknown token is a
// no-op rather than an error — the caller's intent (this token must not work) is
// satisfied either way.
func (s *Store) RevokeToken(ctx context.Context, plaintext string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM user_tokens WHERE token_hash = $1`, hashToken(plaintext)); err != nil {
		return fmt.Errorf("store: revoke token: %w", err)
	}
	return nil
}

// CountTokens reports how many credentials of a kind a user holds. cmd/lyceum
// uses it at boot to decide whether the owner still needs a first sign-in token.
func (s *Store) CountTokens(ctx context.Context, userID int64, kind string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_tokens
		  WHERE user_id = $1 AND kind = $2 AND used_at IS NULL
		    AND (expires_at IS NULL OR expires_at > now())`,
		userID, kind).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count tokens: %w", err)
	}
	return n, nil
}

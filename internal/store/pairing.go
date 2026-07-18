package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PairingTTL bounds how long a short pairing code stays usable (LYCM-88). It is
// deliberately far shorter than InviteTTL: a pairing code is the "right now,
// across the room" carrier for an invite, and its small keyspace means it must
// not linger. The invite token it stands for keeps its own 7-day life.
const PairingTTL = 15 * time.Minute

// pairingAlphabet is Crockford base32 with every ambiguous glyph removed
// outright — no 0/O, 1/I/L, or U — so a code read off one screen and typed on
// another survives the trip. 30 symbols over pairingCodeLen positions is ~39
// bits, which is safe only in company with PairingTTL, single-use redemption,
// and the exchange endpoint's rate limit.
const (
	pairingAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	pairingCodeLen  = 8
)

// newPairingCode returns a fresh random code drawn uniformly from
// pairingAlphabet. Rejection sampling avoids the modulo bias a plain byte%30
// would introduce (256 is not a multiple of 30).
func newPairingCode() (string, error) {
	const maxUnbiased = 256 - (256 % len(pairingAlphabet)) // 240: reject 240..255
	out := make([]byte, 0, pairingCodeLen)
	buf := make([]byte, 1)
	for len(out) < pairingCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("store: generate pairing code: %w", err)
		}
		if int(buf[0]) >= maxUnbiased {
			continue
		}
		out = append(out, pairingAlphabet[int(buf[0])%len(pairingAlphabet)])
	}
	return string(out), nil
}

// normalizePairingCode folds a typed code to the canonical form the hash is
// taken over: upper-cased, with the display hyphen and any spaces removed, and
// anything outside the alphabet dropped. A code that doesn't reduce to exactly
// pairingCodeLen symbols can't match a stored one and is rejected by the caller.
func normalizePairingCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(pairingAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MintInvite issues a single-use invite together with a short, human-typeable
// pairing code that stands for the same invite (LYCM-88). Both plaintexts are
// returned exactly once and are unrecoverable afterwards — only their hashes are
// stored. The invite carries expiresAt (nil meaning no expiry); the pairing code
// always expires after PairingTTL.
func (s *Store) MintInvite(ctx context.Context, userID int64, label string, expiresAt *time.Time) (token string, code string, err error) {
	token, err = newToken()
	if err != nil {
		return "", "", err
	}
	code, err = newPairingCode()
	if err != nil {
		return "", "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tokenID int64
	if err = tx.QueryRow(ctx,
		`INSERT INTO user_tokens (user_id, kind, token_hash, label, expires_at)
		 VALUES ($1, 'invite', $2, $3, $4)
		 RETURNING id`,
		userID, hashToken(token), nullString(label), expiresAt).Scan(&tokenID); err != nil {
		return "", "", fmt.Errorf("store: mint invite: %w", err)
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO pairing_codes (token_id, code_hash, expires_at)
		 VALUES ($1, $2, $3)`,
		tokenID, hashToken(code), time.Now().Add(PairingTTL)); err != nil {
		return "", "", fmt.Errorf("store: mint pairing code: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("store: commit invite: %w", err)
	}
	return token, code, nil
}

// RedeemPairingCode exchanges a short pairing code for a durable session, the
// same way RedeemInvite does for a token: claim the code (single-use, unexpired),
// then claim the invite it stands for, then mint the session — all in one
// transaction. A wrong, spent, or expired code yields ErrNotFound, identical to
// every other failed sign-in, so a probe cannot tell them apart.
func (s *Store) RedeemPairingCode(ctx context.Context, code, deviceLabel string) (User, string, error) {
	norm := normalizePairingCode(code)
	if len(norm) != pairingCodeLen {
		return User{}, "", ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, "", fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Claim the code first: single-use and unexpired, atomically, so two devices
	// racing on the same code cannot both proceed to the invite.
	var tokenID int64
	err = tx.QueryRow(ctx,
		`UPDATE pairing_codes SET used_at = now()
		  WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING token_id`, hashToken(norm)).Scan(&tokenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("store: claim pairing code: %w", err)
	}

	// The underlying invite may already have been spent via its token; claiming
	// it here is the same conditional UPDATE, so that race also resolves to one
	// winner and the loser gets ErrNotFound.
	u, session, err := claimInviteAndMintSession(ctx, tx, "id = $1", tokenID, deviceLabel)
	if err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, "", fmt.Errorf("store: commit redeem: %w", err)
	}
	return u, session, nil
}

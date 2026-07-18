package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewPairingCodeShape(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := newPairingCode()
		if err != nil {
			t.Fatalf("newPairingCode: %v", err)
		}
		if len(code) != pairingCodeLen {
			t.Fatalf("code %q has length %d, want %d", code, len(code), pairingCodeLen)
		}
		for _, r := range code {
			if !strings.ContainsRune(pairingAlphabet, r) {
				t.Fatalf("code %q contains %q, outside the alphabet", code, r)
			}
		}
		// The whole point of the alphabet: none of the ambiguous glyphs.
		if strings.ContainsAny(code, "01ILOU") {
			t.Fatalf("code %q contains an ambiguous glyph", code)
		}
	}
}

func TestNormalizePairingCode(t *testing.T) {
	cases := map[string]string{
		"bk4t9q2m":  "BK4T9Q2M", // lower-cased
		"BK4T-9Q2M": "BK4T9Q2M", // display hyphen stripped
		" bk 4t ":   "BK4T",     // spaces stripped
		"0O1ILU":    "",         // every symbol is outside the alphabet
		"":          "",
	}
	for in, want := range cases {
		if got := normalizePairingCode(in); got != want {
			t.Errorf("normalizePairingCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMintInviteAndRedeemPairingCode(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	member, err := s.CreateUser(ctx, "theo@home.lan", "Theo")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, code, err := s.MintInvite(ctx, member.ID, "invite", nil)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if token == "" || code == "" {
		t.Fatalf("MintInvite returned empty token/code: %q / %q", token, code)
	}

	// Redeem via the code, presented the way a person would type it: hyphenated
	// and lower-cased. Normalization must accept it.
	display := strings.ToLower(code[:4] + "-" + code[4:])
	u, session, err := s.RedeemPairingCode(ctx, display, "Pixel 8")
	if err != nil {
		t.Fatalf("RedeemPairingCode: %v", err)
	}
	if u.ID != member.ID || session == "" {
		t.Fatalf("redeemed as user %d session %q, want user %d with a session", u.ID, session, member.ID)
	}

	// The session actually works.
	if got, err := s.UserByToken(ctx, session); err != nil || got.ID != member.ID {
		t.Fatalf("UserByToken(session) = (%+v, %v), want user %d", got, err, member.ID)
	}

	// Single-use: the same code cannot be redeemed twice.
	if _, _, err := s.RedeemPairingCode(ctx, code, "Another Phone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second RedeemPairingCode err = %v, want ErrNotFound", err)
	}
}

func TestRedeemPairingCodeRejectsWrongOrMalformed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, _, err := s.MintInvite(ctx, ownerID(ctx, t, s), "invite", nil); err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	for _, code := range []string{"BK4T9Q2M", "wrongone", "", "short", "BK4T-9Q2M-EXTRA"} {
		if _, _, err := s.RedeemPairingCode(ctx, code, "dev"); !errors.Is(err, ErrNotFound) {
			t.Errorf("RedeemPairingCode(%q) err = %v, want ErrNotFound", code, err)
		}
	}
}

func TestRedeemPairingCodeRejectsExpired(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_, code, err := s.MintInvite(ctx, ownerID(ctx, t, s), "invite", nil)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	// Force the code past its TTL.
	if _, err := s.pool.Exec(ctx,
		`UPDATE pairing_codes SET expires_at = now() - interval '1 minute'
		  WHERE code_hash = $1`, hashToken(code)); err != nil {
		t.Fatalf("expire code: %v", err)
	}
	if _, _, err := s.RedeemPairingCode(ctx, code, "dev"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired RedeemPairingCode err = %v, want ErrNotFound", err)
	}
}

func TestPairingCodeAndTokenShareOneInvite(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	member, err := s.CreateUser(ctx, "mara@home.lan", "Mara")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, code, err := s.MintInvite(ctx, member.ID, "invite", nil)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	// Redeeming the token spends the single underlying invite...
	if _, _, err := s.RedeemInvite(ctx, token, "Laptop"); err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	// ...so the pairing code that stood for it no longer opens anything.
	if _, _, err := s.RedeemPairingCode(ctx, code, "Phone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RedeemPairingCode after token redeem err = %v, want ErrNotFound", err)
	}
}

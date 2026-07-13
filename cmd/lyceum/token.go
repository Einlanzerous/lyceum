package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// runMintToken issues a one-time sign-in invite for an account (LYCM-801) and
// prints it. This is the recovery path: the server prints an invite for the
// owner on first boot, but only while no session or unredeemed invite exists, so
// if every device is lost — or the printed token scrolled out of the logs — this
// is how you get back in.
//
//	# Invite for the owner:
//	lyceum mint-token
//
//	# Invite for a specific member (e.g. a replacement device):
//	lyceum mint-token --email mara@example.com
//
// Config (DB) comes from the same env as the server.
func runMintToken(args []string) {
	fs := flag.NewFlagSet("mint-token", flag.ExitOnError)
	email := fs.String("email", "", "account to invite (default: the owner)")
	label := fs.String("label", "manual", "label recorded against the token")
	_ = fs.Parse(args)

	cfg := loadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	st := store.New(pool, cfg.dataDir)

	var user store.User
	if *email == "" {
		user, err = st.GetOwner(ctx)
	} else {
		user, err = st.GetUserByEmail(ctx, *email)
	}
	if errors.Is(err, store.ErrNotFound) {
		log.Fatalf("no such account: %s", *email)
	}
	if err != nil {
		log.Fatalf("look up account: %v", err)
	}

	expires := time.Now().Add(store.InviteTTL)
	invite, err := st.MintToken(ctx, user.ID, store.TokenInvite, *label, &expires)
	if err != nil {
		log.Fatalf("mint invite: %v", err)
	}

	// Straight to stdout, not the log: this is the command's output, and it is the
	// only time the token is recoverable.
	log.Printf("one-time sign-in invite for %s (%s):", user.DisplayName, user.Email)
	if _, err := os.Stdout.WriteString(invite + "\n"); err != nil {
		log.Fatalf("write token: %v", err)
	}
}

// bootstrapOwner brings the owner account in line with the configured identity
// and, if nobody can sign in as them yet, mints a first invite and prints it.
//
// Migration 0011 seeds exactly one owner row (adopting all pre-accounts reading
// history) with placeholder details; this is where the operator's
// LYCEUM_OWNER_EMAIL / LYCEUM_OWNER_NAME actually land.
func bootstrapOwner(ctx context.Context, st *store.Store, cfg config) {
	owner, err := st.ReconcileOwner(ctx, cfg.ownerEmail, cfg.ownerName)
	switch {
	case errors.Is(err, store.ErrDuplicateEmail):
		// A typo'd env var shouldn't stop the server booting, but it must not
		// silently do nothing either.
		log.Printf("config: LYCEUM_OWNER_EMAIL=%q already belongs to another account; "+
			"leaving the owner as %s", cfg.ownerEmail, owner.Email)
	case err != nil:
		log.Fatalf("reconcile owner: %v", err)
	}

	sessions, err := st.CountTokens(ctx, owner.ID, store.TokenSession)
	if err != nil {
		log.Fatalf("count owner sessions: %v", err)
	}
	if sessions > 0 {
		return
	}

	// No device is signed in as the owner. If an invite is already outstanding,
	// don't mint a second one on every restart — its plaintext is unrecoverable,
	// so say how to get a fresh one instead.
	invites, err := st.CountTokens(ctx, owner.ID, store.TokenInvite)
	if err != nil {
		log.Fatalf("count owner invites: %v", err)
	}
	if invites > 0 {
		log.Printf("auth: no device is signed in as %s, and an unredeemed invite is "+
			"outstanding. Run `lyceum mint-token` to issue a fresh one.", owner.Email)
		return
	}

	expires := time.Now().Add(store.InviteTTL)
	invite, err := st.MintToken(ctx, owner.ID, store.TokenInvite, "bootstrap", &expires)
	if err != nil {
		log.Fatalf("mint owner invite: %v", err)
	}
	log.Printf("auth: first-boot sign-in invite for %s — %s", owner.Email, invite)
	log.Printf("auth: redeem it once in the app (Settings -> Sign in). It expires in %s and is "+
		"not shown again; `lyceum mint-token` issues another.", store.InviteTTL)
}

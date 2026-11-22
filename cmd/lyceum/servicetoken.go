package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"text/tabwriter"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// runServiceToken is `lyceum service-token mint|revoke|list` (LYCM-140): the only
// way a service token is issued, retired or inspected. It talks to the database
// directly, like mint-token, so it works whether or not LYCEUM_AUTH is on; in the
// deployment it runs as `docker exec lyceum lyceum service-token ...`.
//
//	lyceum service-token mint --label satlink --scope admin:read
//	lyceum service-token revoke --id 3          # or --label satlink
//	lyceum service-token list
func runServiceToken(args []string) {
	cfg := loadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	st := store.New(pool, cfg.dataDir)

	if err := serviceTokenCmd(ctx, st, args, os.Stdout, os.Stderr, cliActor()); err != nil {
		pool.Close()
		log.Fatalf("service-token: %v", err)
	}
}

// cliActor names who ran the command in the audit trail: the OS user, since the
// CLI has no other identity.
func cliActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return "cli:" + u.Username
	}
	return "cli:unknown"
}

// serviceTokenCmd runs one subcommand. The token plaintext goes to out (stdout)
// and nothing else does; everything human-readable goes to info, so
// `lyceum service-token mint ... > file` captures exactly the token.
func serviceTokenCmd(ctx context.Context, st *store.Store, args []string, out, info io.Writer, actor string) error {
	const usage = "usage: lyceum service-token mint --label L --scope S | revoke --id N | --label L | list"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "mint":
		fs := flag.NewFlagSet("service-token mint", flag.ContinueOnError)
		fs.SetOutput(info)
		label := fs.String("label", "", "consumer the token is for (required)")
		scope := fs.String("scope", "", "one of: "+joinScopes()+" (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		id, plaintext, err := st.MintServiceToken(ctx, *label, *scope, actor)
		if err != nil {
			return err
		}
		fmt.Fprintf(info, "minted service token %d for %q (%s); shown once, store it now:\n", id, *label, *scope)
		_, err = fmt.Fprintln(out, plaintext)
		return err

	case "revoke":
		fs := flag.NewFlagSet("service-token revoke", flag.ContinueOnError)
		fs.SetOutput(info)
		id := fs.Int64("id", 0, "revoke the token with this id")
		label := fs.String("label", "", "revoke every live token with this label")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if (*id == 0) == (*label == "") {
			return errors.New("revoke needs exactly one of --id or --label")
		}
		if *id != 0 {
			if err := st.RevokeServiceTokenByID(ctx, *id, actor); err != nil {
				return notFoundAs(err, fmt.Sprintf("no live service token with id %d", *id))
			}
			fmt.Fprintf(info, "revoked service token %d\n", *id)
			return nil
		}
		n, err := st.RevokeServiceTokensByLabel(ctx, *label, actor)
		if err != nil {
			return notFoundAs(err, fmt.Sprintf("no live service token labelled %q", *label))
		}
		fmt.Fprintf(info, "revoked %d service token(s) labelled %q\n", n, *label)
		return nil

	case "list":
		toks, err := st.ListServiceTokens(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tLABEL\tSCOPE\tCREATED\tLAST USED\tREVOKED")
		for _, t := range toks {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.ConsumerLabel, t.Scope,
				t.CreatedAt.UTC().Format(time.RFC3339), orDash(t.LastUsedAt), orDash(t.RevokedAt))
		}
		return tw.Flush()
	}
	return errors.New(usage)
}

func notFoundAs(err error, msg string) error {
	if errors.Is(err, store.ErrNotFound) {
		return errors.New(msg)
	}
	return err
}

func orDash(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func joinScopes() string {
	s := ""
	for i, v := range store.ValidScopes() {
		if i > 0 {
			s += ", "
		}
		s += v
	}
	return s
}

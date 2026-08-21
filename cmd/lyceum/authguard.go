package main

import (
	"context"
	"fmt"
)

// userCounter is the sliver of the store the boot guard needs. Narrowing it to
// the one method keeps the guard testable without a database, which matters
// here more than usual: the failure it exists to prevent is silent, so the
// guard itself is the only thing that can be observed to work.
type userCounter interface {
	CountUsers(ctx context.Context) (int, error)
}

// verifyAuthMode refuses to run a household server in single-user mode
// (LYCM-116).
//
// LYCEUM_AUTH defaults to false and that default is right: a fresh self-host
// should start with no configuration at all. But while it is off, authenticate
// serves *every* request as the owner, so on a server with several accounts all
// of them collapse into one identity — everyone reads the owner's bookmarks and
// (since LYCM-112) the owner's read marks, and their own sync writes land on the
// owner's rows. Nothing errors; the /admin 403 and one line at boot are the only
// outward signs. It is precisely the bug LYCM-112 fixed, reachable by config
// drift rather than by code, and it has already nearly happened: PRSR-10 found a
// local .env carrying no LYCEUM_AUTH at all, one `docker compose up` away from
// bringing prod up collapsed.
//
// So rather than change the default, refuse to start on the state that proves
// the default is wrong for this server: more than one account. A household
// always trips it, since accounts can only be minted while auth is on. A
// single-user install holds only the owner migration 0011 seeds, so it never
// does — and needs no configuration to keep booting.
func verifyAuthMode(ctx context.Context, users userCounter, userAuth bool) error {
	if userAuth {
		return nil
	}
	accounts, err := users.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count accounts: %w", err)
	}
	if accounts <= 1 {
		return nil
	}
	return fmt.Errorf("refusing to start: LYCEUM_AUTH is off but this database holds %d accounts. "+
		"With user auth off every request is served as the owner, so all %d people would share "+
		"the owner's bookmarks and read marks and write their reading positions onto the owner's "+
		"rows. Set LYCEUM_AUTH=true, or delete the extra accounts to run this server single-user.",
		accounts, accounts)
}

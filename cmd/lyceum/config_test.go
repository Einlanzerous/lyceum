package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// testDSN is a DSN shaped like the real one, password and all. Every test here
// asserts that testPassword never comes back out of the code under test.
const (
	testPassword = "not-a-real-password"
	testDSN      = "postgres://lyceum_user:" + testPassword + "@postgres.example.test:5432/lyceum?sslmode=disable"
)

// The usage output must not carry the database password (LYCM-119).
//
// flag prints every flag's DefValue, so registering -database-url with the
// resolved DSN as its default made `lyceum --help` hand the password to anyone
// who could run the binary — and it duly escaped into a transcript, costing a
// password rotation. Asserted here rather than by eye, because the leak is one
// `flag.StringVar(&c.databaseURL, ...)` away from coming back and reads as
// perfectly ordinary code when it does.
func TestUsageOmitsDatabaseCredentials(t *testing.T) {
	t.Setenv("LYCEUM_DATABASE_URL", testDSN)
	t.Setenv("DATABASE_URL", testDSN)

	cfg := envConfig()
	if cfg.databaseURL != testDSN {
		t.Fatalf("envConfig() databaseURL = %q, want the env DSN — the test is not exercising the leak", cfg.databaseURL)
	}

	fs := flag.NewFlagSet("lyceum", flag.ContinueOnError)
	var usage bytes.Buffer
	fs.SetOutput(&usage)
	bindFlags(fs, &cfg)
	fs.PrintDefaults()

	got := usage.String()
	if strings.Contains(got, testPassword) {
		t.Errorf("usage output contains the database password:\n%s", got)
	}
	if strings.Contains(got, testDSN) {
		t.Errorf("usage output contains the full DSN:\n%s", got)
	}
	// The flag still has to be discoverable, and still has to say where its
	// value comes from now that it no longer shows one.
	if !strings.Contains(got, "-database-url") {
		t.Errorf("usage output no longer documents -database-url:\n%s", got)
	}
	if !strings.Contains(got, "LYCEUM_DATABASE_URL") {
		t.Errorf("usage output does not say where the DSN comes from:\n%s", got)
	}
}

// Dropping the flag default must not change where the DSN actually comes from:
// an explicit flag still wins, and the env still fills in when there is none.
func TestDatabaseURLResolution(t *testing.T) {
	const flagDSN = "postgres://flag_user:not-a-real-flag-password@flag.example.test:5432/lyceum"

	cases := []struct {
		name    string
		lyceum  string // LYCEUM_DATABASE_URL
		generic string // DATABASE_URL
		args    []string
		want    string
	}{
		{
			name:   "flag overrides the environment",
			lyceum: testDSN,
			args:   []string{"-database-url", flagDSN},
			want:   flagDSN,
		},
		{
			name:   "environment is used when no flag is given",
			lyceum: testDSN,
			want:   testDSN,
		},
		{
			name:    "DATABASE_URL is the fallback env var",
			generic: testDSN,
			want:    testDSN,
		},
		{
			name:    "LYCEUM_DATABASE_URL wins over DATABASE_URL",
			lyceum:  testDSN,
			generic: "postgres://other_user:not-a-real-other-password@other.example.test:5432/lyceum",
			want:    testDSN,
		},
		{
			name: "built-in default with neither set",
			want: defaultDatabaseURL,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LYCEUM_DATABASE_URL", tc.lyceum)
			t.Setenv("DATABASE_URL", tc.generic)

			cfg := envConfig()
			fs := flag.NewFlagSet("lyceum", flag.ContinueOnError)
			fs.SetOutput(&bytes.Buffer{})
			apply := bindFlags(fs, &cfg)
			if err := fs.Parse(tc.args); err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}
			apply()

			if cfg.databaseURL != tc.want {
				t.Errorf("databaseURL = %q, want %q", cfg.databaseURL, tc.want)
			}
		})
	}
}

// The other flags are seeded from the environment as before; only the DSN gave
// up its default, and only because it is the one that carries a secret.
func TestNonSecretFlagsKeepTheirDefaults(t *testing.T) {
	t.Setenv("LYCEUM_ADDR", ":4005")
	t.Setenv("LYCEUM_DATA_DIR", "/data/blobs")

	cfg := envConfig()
	fs := flag.NewFlagSet("lyceum", flag.ContinueOnError)
	var usage bytes.Buffer
	fs.SetOutput(&usage)
	apply := bindFlags(fs, &cfg)
	fs.PrintDefaults()
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	apply()

	if cfg.addr != ":4005" || cfg.dataDir != "/data/blobs" {
		t.Errorf("addr/dataDir = %q/%q, want :4005//data/blobs", cfg.addr, cfg.dataDir)
	}
	for _, want := range []string{`(default ":4005")`, `(default "/data/blobs")`} {
		if !strings.Contains(usage.String(), want) {
			t.Errorf("usage output lost %s:\n%s", want, usage.String())
		}
	}
}

// The connection-error path is the other way a DSN reaches a log: the server
// log.Fatalf's whatever store.Connect returns. pgx redacts the password in the
// errors it builds, and store.Connect wraps them without re-adding the DSN —
// but that is a property of somebody else's library plus our formatting verbs,
// so pin it rather than assume it survives the next dependency bump.
func TestConnectErrorsOmitPassword(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		// Fails in ParseConfig, before any I/O.
		{name: "unparseable DSN", dsn: "postgres://lyceum_user:" + testPassword + "@postgres.example.test:notaport/lyceum"},
		// Parses, then fails to reach anything. Port 1 refuses immediately.
		{name: "unreachable host", dsn: "postgres://lyceum_user:" + testPassword + "@127.0.0.1:1/lyceum?sslmode=disable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			pool, err := store.Connect(ctx, tc.dsn)
			if err == nil {
				pool.Close()
				t.Fatalf("store.Connect(%q) succeeded; expected a failure to inspect", tc.dsn)
			}
			if strings.Contains(err.Error(), testPassword) {
				t.Errorf("connect error leaks the password: %v", err)
			}
		})
	}
}

// Nor may the DSN reach the startup logs. buildAPIOptions is where boot narrates
// its configuration, and it sits one `%v` away from summarising the config
// struct wholesale.
//
// The config is a literal, not envConfig(): the Makefile does `include .env` +
// `export`, so every variable in a developer's .env is in this process, and
// buildAPIOptions log.Fatalf's on a malformed LYCEUM_API_TOKENS — which exits
// the test binary and fails the whole package without so much as naming the
// test that did it. A literal also makes "which branches logged" a property of
// this file rather than of whoever ran it.
func TestStartupLogsOmitDatabaseCredentials(t *testing.T) {
	// Every branch that narrates itself, turned on, in both its enabled and
	// disabled voice where it has one. SMTP is deliberately left out: setting a
	// host builds a real delivery dispatcher around the store, and this test has
	// no store to give it.
	cfg := config{
		databaseURL:        testDSN,
		addr:               ":4005",
		dataDir:            "/data/blobs",
		userAuth:           true,
		mobileBaseURL:      "https://direct.example.test",
		cfAccessTeamDomain: "example.cloudflareaccess.com",
		cfAccessAUD:        "not-a-real-aud-tag",
		coverFetch:         true,
		coverNormalize:     false,
		ingestQC:           true,
		editionResolve:     true,
		binderyBaseURL:     "http://bindery.example.test:8787",
		binderyAPIKey:      "not-a-real-api-key",
	}

	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	_, cleanup := buildAPIOptions(cfg, nil)
	cleanup()

	got := logs.String()
	if got == "" {
		t.Fatal("buildAPIOptions logged nothing; this test is no longer exercising the startup narration")
	}
	if strings.Contains(got, testPassword) {
		t.Errorf("startup logs contain the database password:\n%s", got)
	}
	if strings.Contains(got, testDSN) {
		t.Errorf("startup logs contain the full DSN:\n%s", got)
	}
}

// The built-in default is the one DSN the usage text still prints verbatim, so
// that it can tell an operator with neither env var set what they are actually
// getting. That is only safe while the constant carries no password — and a
// password is exactly the sort of thing that gets pasted into a constant when
// somebody is debugging a connection. Hold the line here, because nothing else
// would notice: the usage test above drives the *env* DSN, and would pass with a
// credential baked into the source default.
func TestDefaultDatabaseURLCarriesNoPassword(t *testing.T) {
	u, err := url.Parse(defaultDatabaseURL)
	if err != nil {
		t.Fatalf("defaultDatabaseURL does not parse as a URL: %v", err)
	}
	if pw, ok := u.User.Password(); ok {
		t.Errorf("defaultDatabaseURL carries a %d-character password; it is interpolated into "+
			"the -database-url usage text, so this puts it straight back into `lyceum --help`", len(pw))
	}
}

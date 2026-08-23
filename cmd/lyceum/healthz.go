package main

import (
	"encoding/json"
	"net/http"

	"github.com/magos/lyceum/internal/version"
)

// healthzResponse is the body of `GET /healthz`.
//
// ── Why this grew a version and a sha (LYCM-121) ───────────────────────────
//
// Switchyard's delivery reconciler polls this endpoint and records what is
// actually running, which is the observed half of the estate's delivery ledger
// (SWY-192 defines the contract; SERV-128 owns the rollout across services).
// Before these two fields lyceum probed as `no_version`: reachable, speaking,
// but unable to say WHICH build was speaking — so no deploy of lyceum could
// ever be corroborated.
//
// The field names and types are the contract, not a local choice:
//
//	version  bare semver ("1.10.0") or the literal "dev". Never a "v" prefix —
//	         it is compared with strict equality against the image's
//	         org.opencontainers.image.version label, which docker's
//	         metadata-action stamps bare. A prefix here files every deploy
//	         report as `claimed_not_confirmed`, permanently.
//	sha      the full 40-char commit, or JSON null. Never abbreviated: the
//	         cross-service comparison is an equality test, not a prefix match.
//
// `Status` and `Service` keep their existing names and values — uptime-kuma and
// the compose HEALTHCHECK both read this endpoint and neither should care that
// it grew fields.
type healthzResponse struct {
	Status  string  `json:"status"`
	Service string  `json:"service"`
	Version string  `json:"version"`
	SHA     *string `json:"sha"`
}

// handleHealthz answers the liveness probe and the build-identity contract.
//
// Deliberately does not consult Postgres or the blob store. Liveness and
// readiness answer different questions, and a liveness probe that fails on a
// degraded dependency gets the container killed and restarted at exactly the
// moment somebody wants to look at it.
//
// That is also why there is only a 200 path here rather than the 200/503 pair
// the contract permits: lyceum has nothing it would report degraded on. The
// contract's rule is that a 503 must carry the SAME body shape — a degraded
// service is still running a version — so if a readiness verdict is ever added
// it belongs in this struct, on both branches, not in a second shape.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	id := version.Get()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthzResponse{
		Status:  "ok",
		Service: "lyceum",
		Version: id.Version,
		SHA:     id.SHA,
	})
}

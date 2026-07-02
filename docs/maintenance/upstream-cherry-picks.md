# Upstream Cherry-Pick Tracking

This file tracks upstream commits that are cherry-picked, rewritten, partially
absorbed, deferred, or skipped on this repository's local integration branch.

Current local branch: `dev`

Compared refs: `dev` against `upstream/master` (`0942819`)

Last reviewed: 2026-06-20

## Status Legend

| Status | Meaning |
| --- | --- |
| `pending` | Should be absorbed, but no local rewrite has been committed yet. |
| `pending-partial` | Only part of the upstream patch should be absorbed. |
| `pending-group` | Must be absorbed together with related upstream commits. |
| `deferred` | Useful, but not urgent or too broad for the current batch. |
| `skipped` | Intentionally not absorbing as-is. |
| `superseded` | Local code already has a better or equivalent implementation. |
| `rewritten` | Logic has been absorbed into local commit(s) with different hashes. |

## airgate-claude

### Recommended Next

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `32c0d56` | - | `pending` | Mark `/v1/models` as `metadata_only`; small metadata-only route fix with low conflict risk. |
| `91de292` | - | `pending` | Declare plugin `error_format=anthropic` so Core can emit Anthropic-shaped route errors. |
| `f8f41c2` | - | `pending` | Fill usage cost in `streamAbortedOutcome`; current `dev` still returns aborted stream usage without calling `fillUsageCost`, so interrupted streams can record tokens with zero cost. |
| `88b1016` | - | `pending-partial` | Absorb JSON-safe OAuth handler errors and interleaved-thinking preservation. Compare the OAuth session cap carefully: local `dev` already has a high-water session store cleanup, but with different limits and policy. |
| `0942819` | - | `pending` | Use `cssVar('primaryForeground')` for `AccountForm` primary buttons; current `dev` still hard-codes `color: 'white'`. |

### API Key / Tool-Calling Batch

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `ec624cc` | - | `pending-group` | API Key requests should use lossless/minimal body preprocessing. Current `dev` still runs API Key bodies through `preprocessBody` plus `preprocessOAuthBody`, which can rewrite tools, thinking blocks, and escaped strings. Rewrite with the related header fixes below. |
| `c3e37cb` | `6d43808` (partial) | `pending-partial` | SSE max line size is already 8 MB locally. Still absorb API Key `anthropic-beta` passthrough; current `dev` still filters client betas. |
| `9039b7a` | `3d57b0d`, `daeff34`, `a189923` (partial) | `pending-partial` | Local `dev` already has stronger token refresh state cleanup/error propagation, pooled usage transport, and SOCKS deadline handling. Still evaluate and absorb API Key raw header passthrough, `X-Forwarded-Path` path resolution, and removal of the extra API Key `Authorization` header from `count_tokens`. |

### Already Absorbed / Rewritten

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `afc6874` | `5a0c72a` | `rewritten` | Same Fable 5 model and Opus 4.8 fallback patch, with different author metadata. `git cherry` reports this as equivalent. |
| `05016a0` | `6d43808`, `bda6171` | `rewritten` | Local `dev` already removes total timeout from streaming clients and adds SSE idle guarding. Upstream's configurable `default_timeout` / `first_byte_timeout` / `stream_idle_timeout` knobs are not absorbed; keep as optional follow-up, not a direct cherry-pick. |

### Second Batch

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `6ed46c8` | `251d1d0`, `300a742`, `35770dd`, `f4c8d25`, `c125ae9`, `84bd68c` (partial) | `deferred` | Local usage display has already diverged and covers much of the cache token presentation. Consider extracting only the friendlier cache cost labels and WSL watch polling if needed. |

### Documentation / Tooling

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `47e3a98` | - | `deferred` | Tooling and lint baseline commit (`golangci`, ESLint, commit hook, CLAUDE.md, Makefile). Do not cherry-pick as-is without reviewing local SDK/package manager versions and existing Go 1.26 setup. |
| `f6021ba` | - | `deferred` | CLAUDE.md documentation cleanup. Useful, but not runtime-critical. |

### High Conflict / Do Not Apply As-Is

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `ec624cc` | - | `pending-group` | Listed above because it is important, but it touches the same API Key/OAuth preprocessing surface as local gateway rewrites. Apply as a local rewrite with tests, not as an isolated cherry-pick. |
| `9039b7a` | - | `pending-partial` | Broad mixed fix. Several parts are already superseded locally, while the API Key header/path/count-token fixes remain useful. Extract specific hunks only. |

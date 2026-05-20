# MCP Release Follow-ups Plan

**Date:** 2026-05-20
**Context:** Post-merge / Phase C wrap-up discovered five outstanding items. This plan groups them and executes in dependency order.

## Tasks

### Task 1: Publish `gopedia` package + chain optional gopedia-mcp dep

**Goal:** (a) Get `gopedia` into the hub so it satisfies FK for any package wanting to depend on it. (b) Offer gopedia-mcp as an optional install when users install `gopedia`.

**Files / actions (data-only, no code):**

- `POST /packages` with `slug=gopedia`. Reasonable defaults:
  - type: `binary` (gopedia is a Go service binary; npm/pip don't fit)
  - description: "Gopedia HTTP API — knowledge graph search/ingest service"
  - recommended_strategy: `k8s` (it's actually deployed as a k8s workload in this org)
  - tags: `["knowledge", "search", "rag"]`
- `PUT /packages/gopedia/dependencies` body:
  ```json
  {"dependencies":[{"slug":"gopedia-mcp","min_version":"","optional":true}]}
  ```
  - FK now resolves (gopedia-mcp exists since Phase C).
  - When users install gopedia, optional dep prompt fires for gopedia-mcp.
- `PUT /packages/gopedia/install-script` for at least one strategy (k8s or docker). For now, a minimal placeholder script that echoes "see gopedia repo for cluster deploy" — fully-baked install scripts are a separate effort.

**Acceptance:**
- `GET /packages/gopedia` returns the metadata
- `GET /packages/gopedia/dependencies` shows gopedia-mcp with `optional:true`
- (manual) Running `moso install gopedia` triggers the optional dep prompt

**Known limitation:** the prompt copy in `cmd/install.go::promptOptionalDep` is hardcoded for LLM-config scenarios ("LLM 설정을 선택하세요"). Result feels off for an MCP dep but functions. → Task 5.

---

### Task 2: Add `gopedia` as required dep on `gopedia-mcp`

**Goal:** Complete the dep loop originally intended in Phase C (was blocked by FK; now unblocked).

**Action:**
```bash
curl -X PUT $HUB/packages/gopedia-mcp/dependencies \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"dependencies":[{"slug":"gopedia","min_version":"","optional":false}]}'
```

**⚠️ Circular dep risk:** With Task 1, `gopedia` has `gopedia-mcp` as optional. With Task 2, `gopedia-mcp` has `gopedia` as required. That's a soft cycle (required ↔ optional). Check current `BuildDependencyPlan` (in `internal/hub/deps.go`) handles this — if it walks deps transitively and an optional dep itself has a back-ref required dep, does it loop or get short-circuited by install history?

**Decision gate:** before doing this, read `deps.go` and confirm:
- If it can loop: don't add this dep; document as known cycle limitation
- If it's safe (install history short-circuit, or no transitive walk of optional deps): proceed

**Acceptance:**
- `GET /packages/gopedia-mcp/dependencies` shows `gopedia` required
- (manual) `moso install gopedia-mcp` triggers the required-dep flow for gopedia first

---

### Task 3: Fix ArgoCD image-updater config

**Symptom:** `argocd-image-updater` logs (Wed 06:42:03):
```
cannot use update strategy 'digest' for image '...morphso-hub' without a version constraint
```
And similar errors for many other apps (gardener, goquest, lymphhub, mrebel, ...). morphso-hub deployment never auto-updates as a result; I worked around with manual `kubectl rollout restart`.

**Fix options:**
- (preferred) Annotate each Application with a version constraint:
  ```yaml
  argocd-image-updater.argoproj.io/image-list: "app=artifacts.toji.homes/neunexus/morphso-hub:~latest"
  ```
  The `:~latest` matches the moving `:latest` tag for digest tracking.
- (alternative) Switch update-strategy from `digest` to `newest-build` and use `tag-match` annotation. Less precise but simpler.

**Action:**
1. Audit affected Applications:
   ```bash
   kubectl -n metaflow logs deploy/argocd-image-updater --tail=500 | grep -B1 "digest.*without a version constraint" | grep -oP 'application=\S+' | sort -u
   ```
2. For each, patch the annotation (one PR per app's manifest repo or via `kubectl annotate` if they're manifest-stored). Start with morphso-hub as the proof-of-concept.
3. Wait one image-updater loop (2 min by default); check the next reconcile clears the error.

**Acceptance:**
- morphso-hub Application annotation includes version constraint
- image-updater log no longer shows "without a version constraint" for morphso-hub
- Trigger a fresh image push (e.g. trivial CI re-run); verify ArgoCD updates deployment automatically without `rollout restart`

**Scope note:** Fixing every affected app is a broader infra task. Scope here is morphso-hub only; the rest is documented for a separate sweep.

---

### Task 4: Configure Woodpecker `github_token` secret for morphso

**Symptom:** v0.1.0 release CI step `release` (goreleaser) failed even after fixing image + config — likely because `github_token` secret isn't registered in Woodpecker for `tojiuni/morphso`. Confirmed by: local goreleaser with `GITHUB_TOKEN=$(gh auth token)` succeeded; the only delta is the secret.

**Action:**
1. Create a GitHub Personal Access Token (or fine-grained token) for `tojiuni/morphso`:
   - Scopes: `contents:write` (release upload), `metadata:read`
   - Expiration: 90 days (reasonable cadence; or longer if rotated externally)
2. Add as Woodpecker secret:
   - Either via Woodpecker UI (https://ci.toji.homes → repo morphso → Secrets → Add)
   - Or via CLI: `woodpecker-cli secret add --repo=tojiuni/morphso --name=github_token --value=<token> --event=tag`
   - Restrict to `tag` event so PR pipelines can't read it
3. Verify: push a tag like `v0.1.1-rc1` to a throwaway test branch (or re-tag v0.1.0 if acceptable; we already have local release). Confirm `release` step succeeds.

**Alternative:** maintain release as a local-only operation (current state). Document in CONTRIBUTING that releases are manual until secret is set.

**Acceptance:**
- Secret visible in Woodpecker UI for the repo
- A tag-triggered pipeline completes the release step successfully

---

### Task 5: Generalize optional-dep prompt for non-LLM deps

**Symptom:** `cmd/install.go::promptOptionalDep` is hardcoded to LLM-config phrasing:
```go
fmt.Printf("\n[optional] %s — LLM 설정을 선택하세요:\n", depName)
fmt.Printf("  [1] %s 신규 설치 (Docker)\n", depName)
fmt.Printf("  [2] 기존 %s URL 입력\n", depName)
fmt.Printf("  [3] API 토큰 입력 (OpenAI / Anthropic / 기타)\n")
```

For gopedia-mcp as an optional dep on gopedia (Task 1), this UI is wrong — there's no URL or API token alternative; the choice is binary (install or skip).

**Design (lightweight):**
- Add a `kind` hint to optional dep metadata (hub side, new field on dependency record): `"llm"` (default for backward-compat) | `"mcp"` | `"plain"`
- Or infer from `depPkg.Type`: if `mcp` → render simplified "[1] install, [2] skip"; if `binary`/`npm` → same simple flow; only when `pkg has env_schema` showing LLM-shape values → use full 4-option LLM flow.

Concrete approach: refactor `promptOptionalDep` into a strategy function that dispatches on `depPkg.Type`. MCP/binary/recipe get a 2-option prompt; only npm packages with specific env_schema shape (or explicit `dep.kind=="llm"` annotation later) get the LLM prompt.

**Files:**
- Modify: `cmd/install.go` — refactor `promptOptionalDep`
- Add: test cases in `cmd/install_mcp_test.go` covering "optional MCP dep prompts simple install/skip"

**Acceptance:**
- `moso install gopedia` shows a 2-option prompt for gopedia-mcp ("install / skip"), not the 4-option LLM prompt
- `moso install gopedia-pro` (or any hypothetical npm+LLM dep) still shows the 4-option LLM prompt
- Tests pass; no regression in existing optional-dep tests

**Estimated effort:** 1-2 hours including tests.

---

### Task 6: goreleaser config audit

**Symptom:** v0.1.0 release tripped over `archives.format` deprecation. Goreleaser v2.x has more deprecations that will land as future broken pipelines.

**Action:**
1. Run `goreleaser check --verbose` locally with the latest goreleaser; capture all warnings.
2. Migrate any remaining deprecated fields (likely candidates: `before.hooks` formatting, `release.github` vs `release` top-level, snapshot template variables).
3. Pin `.woodpecker.yml` image tag to a specific goreleaser version going forward; document the upgrade procedure (run `goreleaser check` after bumping).

**Acceptance:**
- `goreleaser check --verbose` produces zero DEPRECATED warnings
- `.woodpecker.yml` uses a specific version
- README or CONTRIBUTING has a "release process" section noting the version-pin

**Scope:** small. Likely a single follow-up commit.

---

## Execution Order

```
1 (publish gopedia + optional dep) ─┐
                                    ├─> 2 (add gopedia as required dep on gopedia-mcp — IF deps.go is cycle-safe)
                                    │
3 (ArgoCD)   — independent          │
4 (Woodpecker secret) — independent │
5 (prompt refactor) — independent, but UX tied to 1 │
6 (goreleaser audit) — independent  │
```

Tasks 3, 4, 6 are infra/config and can run in parallel with 1, 2, 5. Tasks 1+2+5 form the user-facing "install gopedia and gopedia-mcp together" flow.

## Non-Goals / Out of Scope

- Publishing gopedia's full install scripts (k8s manifests etc.) — that needs its own design pass with the gopedia maintainers.
- Migrating all 15+ apps from Task 3's audit — only morphso-hub here; the rest is a separate infrastructure sweep ticket.
- Replacing the JWT auth with refresh tokens (no Phase-C re-run wanted device flow) — orthogonal upgrade.

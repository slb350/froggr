# froggr

## Project Description

A GitHub App that reviews code iteratively during development — before a PR is opened. On every push to a branch matching an issue number, froggr posts structured findings in the issue thread. When clean, it opens a draft PR automatically.

## Repository Structure

```
froggr/
├── cmd/froggr/          # Entry point, dependency wiring (main.go + main_test.go)
├── internal/
│   ├── ai/              # Provider-agnostic types (Message, CompletionRequest, Role); DefaultHTTPTimeout (120s) shared by all AI provider clients
│   ├── bedrock/         # AWS Bedrock Converse API client
│   ├── config/          # .froggr.yml parsing, branch pattern matching, provider defaults (DefaultsForProvider/DefaultsForProviders, ParseWithDefaults, Bedrock ARN support)
│   ├── debounce/        # Timer-based push debounce (30s window): buffer.go + buffer_test.go
│   ├── ghub/            # GitHub App auth, webhook parsing, API client, types; per-installation AppAuth client caching; IsNotFound helper; SignatureError (401 vs 400)
│   ├── openrouter/      # OpenRouter chat completion HTTP client
│   ├── review/          # AI review engine: engine, interfaces, types, context, prompt, parse (ErrInvalidAIResponse sentinel), format, errors (SuppressFailureComment, ShouldPostFailureComment)
│   ├── server/          # HTTP server, webhook routing, event handler
│   └── testutil/        # Shared test helpers (webhook signing, error fixtures)
├── docs/
│   └── design.md        # Design decisions
├── go.mod
├── go.sum
├── .golangci.yml        # golangci-lint v2 config (12 linters + gofmt/goimports)
├── LICENSE
├── README.md
└── justfile             # Task runner (fmt, lint, test, check, build)
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go 1.26.1+ |
| **AI (default)** | OpenRouter (HTTP, OpenAI-compatible) |
| **AI (alt)** | AWS Bedrock (Converse API, standard credential chain) |
| **Hosting** | Self-hosted GitHub App |

## Development Workflow

- TDD: Write failing tests first, implement, refactor, commit
- All tests must pass before committing — pre-commit hook runs `just check`
- **Never skip `--no-verify`**

## Common Commands

```bash
# Build
just build
# or: go build -o bin/froggr ./cmd/froggr

# Run directly
go run ./cmd/froggr

# Run tests (with race detector)
go test ./... -race -count=1

# Format (requires goimports)
just fmt

# Lint (requires golangci-lint v2)
just lint
# or: golangci-lint run ./...

# Full check (format, lint, test) — same as pre-commit hook
just check
```

## Configuration

Users configure froggr via `.froggr.yml` in their repo root:

```yaml
branch_pattern: "^(\\d+)-"   # extract issue number from branch name
auto_draft_pr: true
ignore_paths:
  - "*.lock"
  - ".env*"
  - "vendor/**"
  - "generated/**"
provider: "openrouter"
model: "anthropic/claude-sonnet-4.6"
```

If `.froggr.yml` is missing, froggr uses provider-aware server defaults: OpenRouter when configured, Bedrock in Bedrock-only installs. If GitHub cannot read the config for any other reason, froggr skips the review rather than silently changing review policy.

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_PRIVATE_KEY` | Yes | PEM-encoded private key |
| `GITHUB_WEBHOOK_SECRET` | Yes | HMAC secret for webhook validation |
| `OPENROUTER_API_KEY` | If using OpenRouter | OpenRouter API key |
| `AWS_REGION` | If using Bedrock | AWS region (`AWS_DEFAULT_REGION` also accepted) |
| `PORT` | No | Server port (default: `8080`) |

At least one AI provider must be configured.

## Key Design Decisions

### Review Budgeting
Review context is deliberately bounded to keep large pushes fast and predictable:
- At most **25 changed-file contexts** per review — applied *after* `ignore_paths` filtering, so ignored files don't count against the cap
- File contents are fetched in parallel (up to **10 concurrent** GitHub API requests) to minimize review latency
- At most **5 most recent prior froggr reviews** — identified by checking `c.GetUser() != nil && strings.HasSuffix(c.GetUser().GetLogin(), froggrBotSuffix)` (the `froggrBotSuffix` constant); `shouldIncludePriorReview` then filters out any comment whose body is blank, or whose body starts with `"Review failed:"` / `"Review skipped."` (`HasPrefix`) **or** contains either marker after a blank-line section break (`strings.Contains(trimmed, "\n\n"+marker)`) — both conditions apply to both markers
- All issue comments are fetched (paginated, 100/page) before filtering; the last 5 surviving froggr comments are selected
- Oversized issue bodies, patches, file contents, and prior review text are truncated with UTF-8-safe byte budgeting: issue body 4,000 bytes, diff patch 8,000 bytes, file content 12,000 bytes, prior review 4,000 bytes
- Final prompt is capped at 120,000 bytes (~30k tokens); the model is told when context was omitted
- OpenRouter responses exceeding 2 MB are rejected before parsing

### Fail-Closed Behavior
- If a branch comparison reaches GitHub's 300 changed-file limit, froggr **refuses the review** and posts an explanatory comment (rather than claiming a partial diff was complete)
- If a review fails (AI timeout, rate limit, etc.), froggr **posts a failure comment** so the developer knows and can push again to retry
- Certain non-actionable error conditions (closed issue, comparison-too-large, **draft PR creation failure**) are wrapped with `review.SuppressFailureComment` — froggr skips posting the failure comment for these so as not to generate noise. Check `review.ShouldPostFailureComment(err)` before posting. Several push types are filtered without starting a review: tag pushes (`refs/tags/`) and deleted branch pushes cause `ExtractPushContext` to return an error, which `routeEvent` logs as `"ignoring push event"`; default branch pushes are detected after `ExtractPushContext` succeeds and are logged separately as `"ignoring push to default branch"` inside `HandlePush`.
- AI response parsing uses a three-tier strategy: bare JSON array → fenced JSON (markdown code block) → text pattern matching. An explicit empty JSON array `[]` is the only way to signal "clean". Ambiguous or malformed output that matches none of the tiers fails the run rather than being treated as clean
- Draft PR creation is idempotent: if GitHub returns 422 "already exists", `CreateDraftPR` calls `findExistingPullRequest` and returns the existing PR rather than failing
- Skipped reviews (`"Review skipped."`) and failed reviews (`"Review failed: …"`) post distinct comment types — skipped means a structural limit was hit (retry won't help without a push); failed means a transient error occurred and pushing again triggers a retry

### AI Finding Format
The AI must return findings as a JSON array. Each finding object must have exactly these fields:

```json
{"severity": "Bug" | "Concern", "file": "path/to/file.go", "line": 42, "description": "..."}
```

An explicit empty array `[]` signals a clean review. Findings are sorted `Bug` first, `Concern` second when formatted as issue comments.

### Timeouts
- **GitHub API client**: 30s per request (`defaultGitHubTimeout`)
- **AI provider HTTP client**: 120s per request (`ai.DefaultHTTPTimeout`, shared by both OpenRouter and Bedrock clients)
- **Review run**: 3 min hard timeout; failure comment posted on expiry (with its own 30s context)
- **Provider initialization**: 15s (`providerInitTimeout`)
- **HTTP server read-header**: 10s
- **Graceful shutdown drain**: 10s (`shutdownTimeout`)

### Provider Auto-Detection Failure Modes
If `provider` is omitted and the `model` field has no slash (OpenRouter indicator), no dot, and is not an ARN, `detectProvider` returns an explicit error: `cannot auto-detect provider for model "…": set provider explicitly in .froggr.yml`. This is a hard failure, not a fallback.

### Push Debounce
The `debounce` package provides a 30-second window to coalesce rapid successive pushes into a single review run. Multiple branches per issue are tracked concurrently — the server maintains a `map[issueRef]map[debounce.Key]struct{}` so each branch gets its own debounce key. Each review run has a 3-minute hard timeout; if the AI or GitHub API stalls beyond that, the review fails and a failure comment is posted (with a 30-second timeout of its own, using a fresh context so it works even when the review timed out). Provider initialization is bounded to 15 seconds (`providerInitTimeout`) to prevent startup hangs when AWS IMDS is unreachable; if one provider fails but another succeeds, the working provider is used.

### Provider Auto-Detection
If `provider` is omitted in `.froggr.yml`, froggr auto-detects the provider from the `model` field (OpenRouter uses slash notation; Bedrock uses dotted IDs or ARN-based model refs — inference profiles, prompt versions, provisioned/custom models, and SageMaker endpoint ARNs `arn:aws:sagemaker:...:endpoint/...` are all recognized). Model IDs containing both a slash and a dot (e.g. `anthropic/claude-sonnet-4.6`) are classified as OpenRouter because the slash check takes precedence over the dot check. Repos that omit both inherit defaults from whatever providers are available on the server (`DefaultsForProvider`/`DefaultsForProviders` in `config/`). Server-chosen defaults are merged into per-repo config via `config.ParseWithDefaults`.

When `provider` is set explicitly, `validateProviderModel` enforces cross-validation: an OpenRouter provider rejects Bedrock-style model IDs and vice versa. This is a hard config error, not a warning.

### Webhook Events Handled
- `push` — triggers a debounced review for branches matching an issue number
- `issues.closed` — cancels any pending review for the closed issue

### Endpoints
- `POST /webhook` — GitHub webhook receiver (HMAC validated)
- `GET /health` — Liveness/readiness probe

## Conventions
- All styles and config via injected dependencies (no package-level globals)
- Graceful shutdown: in-flight reviews are canceled to prevent stale upstream hangs
- GitHub API calls use a client timeout throughout

# froggr

## Project Description

A GitHub App that reviews code iteratively during development — before a PR is opened. On every push to a branch matching an issue number, froggr posts structured findings in the issue thread. When clean, it opens a draft PR automatically.

## Repository Structure

```
froggr/
├── cmd/froggr/          # Entry point, dependency wiring
├── internal/
│   ├── ai/              # Provider-agnostic types (Message, CompletionRequest, Role)
│   ├── bedrock/         # AWS Bedrock Converse API client
│   ├── config/          # .froggr.yml parsing, branch pattern matching, provider defaults (DefaultsForProvider/DefaultsForProviders, ParseWithDefaults, Bedrock ARN support)
│   ├── debounce/        # Timer-based push debounce (30s window)
│   ├── ghub/            # GitHub App auth, webhook parsing, API client, types; per-installation AppAuth client caching
│   ├── openrouter/      # OpenRouter chat completion HTTP client
│   ├── review/          # AI review engine: engine, interfaces, types, context, prompt, parse, format, errors
│   ├── server/          # HTTP server, webhook routing, event handler
│   └── testutil/        # Shared test helpers (webhook signing, error fixtures)
├── docs/
│   └── design.md        # Design decisions
├── go.mod
├── go.sum
└── justfile             # Task runner (fmt, lint, test, check, build)
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| **Language** | Go 1.26+ |
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
- At most **25 changed-file contexts** per review
- File contents are fetched in parallel (up to **10 concurrent** GitHub API requests) to minimize review latency
- At most **5 most recent prior froggr reviews** (excluding comments starting with `"Review failed:"` or containing `"Review skipped."` — these are filtered by `shouldIncludePriorReview` before building the prompt)
- Oversized issue bodies, patches, file contents, and prior review text are truncated with UTF-8-safe byte budgeting
- Final prompt is capped at a fixed size; the model is told when context was omitted

### Fail-Closed Behavior
- If a branch comparison reaches GitHub's 300 changed-file limit, froggr **refuses the review** and posts an explanatory comment (rather than claiming a partial diff was complete)
- If a review fails (AI timeout, rate limit, etc.), froggr **posts a failure comment** so the developer knows and can push again to retry
- Certain non-actionable error conditions (closed issue, comparison-too-large, deleted branch) are wrapped with `review.SuppressFailureComment` — froggr skips posting the failure comment for these so as not to generate noise. Check `review.ShouldPostFailureComment(err)` before posting.
- AI response parsing uses a three-tier strategy: bare JSON array → fenced JSON (markdown code block) → text pattern matching. An explicit empty JSON array `[]` is the only way to signal "clean". Ambiguous or malformed output that matches none of the tiers fails the run rather than being treated as clean

### Push Debounce
The `debounce` package provides a 30-second window to coalesce rapid successive pushes into a single review run. Multiple branches per issue are tracked concurrently — the server maintains a `map[issueRef]map[debounce.Key]struct{}` so each branch gets its own debounce key. Each review run has a 3-minute hard timeout; if the AI or GitHub API stalls beyond that, the review fails and a failure comment is posted (with a 30-second timeout of its own, using a fresh context so it works even when the review timed out). Provider initialization is bounded to 15 seconds (`providerInitTimeout`) to prevent startup hangs when AWS IMDS is unreachable; if one provider fails but another succeeds, the working provider is used.

### Provider Auto-Detection
If `provider` is omitted in `.froggr.yml`, froggr auto-detects the provider from the `model` field (OpenRouter uses slash notation; Bedrock uses dotted IDs or ARN-based model refs — inference profiles, provisioned/custom models, and marketplace endpoint ARNs are all recognized). Repos that omit both inherit defaults from whatever providers are available on the server (`DefaultsForProvider`/`DefaultsForProviders` in `config/`). Server-chosen defaults are merged into per-repo config via `config.ParseWithDefaults`.

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

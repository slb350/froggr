# froggr

## Project Description

A GitHub App that reviews code iteratively during development — before a PR is opened. On every push to a branch matching an issue number, froggr posts structured findings in the issue thread. When clean, it opens a draft PR automatically.

## Repository Structure

```
froggr/
├── cmd/froggr/          # Entry point, dependency wiring
├── internal/
│   ├── ai/              # Provider-agnostic types (Message, CompletionRequest, interfaces.go)
│   ├── bedrock/         # AWS Bedrock Converse API client
│   ├── config/          # .froggr.yml parsing, branch pattern matching
│   ├── debounce/        # Timer-based push debounce (30s window)
│   ├── ghub/            # GitHub App auth, webhook parsing, API client
│   ├── openrouter/      # OpenRouter chat completion HTTP client
│   ├── review/          # AI review engine (context → prompt → parse → format)
│   ├── server/          # HTTP server, webhook routing, event handler
│   └── testutil/        # Shared test helpers (webhook signing, error fixtures)
├── docs/
│   └── design.md        # Design decisions
├── go.mod
├── go.sum
└── justfile             # Task runner
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
# Build and run
go build -o bin/froggr ./cmd/froggr
./bin/froggr

# Or run directly
go run ./cmd/froggr

# Run tests (with race detector)
go test ./... -race -count=1

# Lint (requires golangci-lint v2)
golangci-lint run

# Full check (format, lint, test)
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
- At most **5 most recent prior froggr reviews** (excluding failed/skipped)
- Oversized issue bodies, patches, file contents, and prior review text are truncated with UTF-8-safe byte budgeting
- Final prompt is capped at a fixed size; the model is told when context was omitted

### Fail-Closed Behavior
- If a branch comparison reaches GitHub's 300 changed-file limit, froggr **refuses the review** and posts an explanatory comment (rather than claiming a partial diff was complete)
- If a review fails (AI timeout, rate limit, etc.), froggr **posts a failure comment** so the developer knows and can push again to retry
- Malformed or off-format AI output fails the run — froggr only accepts an explicit empty JSON array (clean) or structured, validated findings

### Push Debounce
The `debounce` package provides a 30-second window to coalesce rapid successive pushes into a single review run.

### Provider Auto-Detection
If `provider` is omitted in `.froggr.yml`, froggr auto-detects the provider from the `model` field (OpenRouter uses slash notation; Bedrock uses dotted IDs). Repos that omit both inherit defaults from whatever providers are available on the server.

### Endpoints
- `POST /webhook` — GitHub webhook receiver (HMAC validated)
- `GET /health` — Liveness/readiness probe

## Conventions
- All styles and config via injected dependencies (no package-level globals)
- Graceful shutdown: in-flight reviews are canceled to prevent stale upstream hangs
- GitHub API calls use a client timeout throughout

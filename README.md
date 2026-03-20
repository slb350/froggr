# froggr

Navigate your code through traffic to a safe place.

**froggr** is a GitHub App that reviews your code *while you're still writing it* — not after you open a PR, when you've already context-switched to the next thing. Think of it like Frogger: your code hops through lanes of review (states) to safely reach the other side (a clean draft PR).

## How It Works

1. **Create a GitHub Issue** describing the work
2. **Push to a branch** matching the issue (`42-add-auth` links to Issue #42)
3. **froggr reviews your code** on every push and posts findings in the issue thread
4. **Fix, push, repeat** — froggr re-reviews, tracking what's been resolved
5. **When clean, froggr opens a draft PR** linked to the issue automatically
   If that PR already exists on a later clean push, froggr reuses it instead of failing on duplicate creation.

```
Issue #42: Add user authentication
│
├── Push 1 → Review: "Bug in token expiry, missing error handler"
├── Push 2 → Review: "Token fixed, error handler still missing"
├── Push 3 → Review: "All clear"
│
└── Draft PR #51 opened → Closes #42
```

## The Gap

| Tool | When it reviews | The problem |
|------|----------------|-------------|
| Greptile / CodeRabbit | After PR is opened | Too late — you've context-switched |
| Claude Code / Copilot | While writing code | Full agent mode — writes for you |
| **froggr** | **During development, before PR** | **Reviews iteratively as you work** |

## Configuration

Add a `.froggr.yml` to your repo root:

```yaml
# .froggr.yml
branch_pattern: "^(\\d+)-"   # extract issue number from branch name
auto_draft_pr: true           # open draft PR when review is clean
ignore_paths:
  - "*.lock"
  - "vendor/**"
  - "generated/**"

# Any model available on OpenRouter
# See: https://openrouter.ai/models
model: "anthropic/claude-sonnet-4.6"
```

If `.froggr.yml` is missing, froggr uses defaults. If GitHub cannot read the
file for some other reason, froggr skips the review rather than silently
changing review policy.

froggr uses [OpenRouter](https://openrouter.ai) under the hood, so you can use any model — Claude, GPT-5, Gemini 3, Qwen 3.5, MiniMax, or whatever suits your codebase and budget.

**Popular models for code review:**
- `anthropic/claude-sonnet-4.6` — strong reasoning, good cost/quality balance (default)
- `anthropic/claude-opus-4.6` — best quality, higher cost
- `openai/gpt-5.3-codex` — purpose-built for code
- `google/gemini-3.1-pro-preview` — large context, strong reasoning
- `qwen/qwen3.5-397b-a17b` — massive 397B MoE, top-tier reasoning
- `minimax/minimax-m2.7` — fast, strong general reasoning

## Self-Hosting

### Prerequisites

- Go 1.22+
- A [GitHub App](https://docs.github.com/en/apps/creating-github-apps) with:
  - Webhook URL pointing to your server's `/webhook` endpoint
  - Permissions: Issues (read/write), Pull requests (read/write), Contents (read)
  - Events: Push, Issues
- An [OpenRouter API key](https://openrouter.ai/keys)

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_APP_ID` | Yes | Your GitHub App's ID |
| `GITHUB_PRIVATE_KEY` | Yes | PEM-encoded private key for the GitHub App |
| `GITHUB_WEBHOOK_SECRET` | Yes | HMAC secret for webhook signature validation |
| `OPENROUTER_API_KEY` | Yes | API key for OpenRouter |
| `PORT` | No | Server port (default: `8080`) |

### Run

```bash
# Build and run
go build -o bin/froggr ./cmd/froggr
./bin/froggr

# Or run directly
go run ./cmd/froggr
```

froggr exposes two endpoints:
- `POST /webhook` — GitHub webhook receiver
- `GET /health` — liveness/readiness probe

Review runs are bounded. GitHub API calls use a client timeout, and in-flight
reviews are canceled during shutdown so a stalled upstream cannot hang the
service indefinitely.

Model output is validated strictly. froggr only accepts an explicit empty JSON
array for a clean review or structured findings it can validate; malformed or
off-format AI output fails the run instead of being treated as "all clear."

### Local Development

```bash
# Run tests
go test ./... -race -count=1

# Lint (requires golangci-lint v2)
golangci-lint run

# Full check (format, lint, test)
just check

# Expose localhost for GitHub webhooks
# Use smee.io or ngrok to forward to http://localhost:8080/webhook
```

## Architecture

```
cmd/froggr/          → entry point, dependency wiring
internal/config/     → .froggr.yml parsing, branch pattern matching
internal/openrouter/ → OpenRouter chat completion HTTP client
internal/ghub/       → GitHub App auth, webhook parsing, API client
internal/debounce/   → timer-based push debounce (30s window)
internal/review/     → AI review engine (context → prompt → parse → format)
internal/server/     → HTTP server, webhook routing, event handler
internal/testutil/   → shared test helpers (webhook signing, error fixtures)
```

## Review Budgeting

froggr keeps review context deliberately bounded so large pushes stay fast and
predictable instead of timing out or relying on provider-side truncation.

- It fetches at most 25 changed-file contexts per review
- It includes at most the 5 most recent prior froggr reviews
- It truncates oversized issue bodies, patches, file contents, and prior review text
- It caps the final model prompt at a fixed size and tells the model when context was omitted

This is an explicit tradeoff: on very large pushes, froggr prefers a smaller,
stable review packet over an unbounded prompt that is slow, expensive, and
more likely to fail formatting or be silently clipped upstream.

froggr also fails closed on GitHub's compare-file ceiling. If a branch
comparison reaches GitHub's 300 changed-file limit, froggr will refuse the
review rather than claim a partial diff was fully reviewed, and it posts an
issue comment explaining why the review was skipped.

See [docs/](./docs/) for detailed design decisions.

## License

MIT

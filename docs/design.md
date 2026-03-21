# froggr Design Document

## Elevator Pitch

AI code review happens too late — after you open a PR and context-switch to the next thing. **froggr** reviews your code while you're still writing it. Link a branch to a GitHub Issue, push your work, and get AI review feedback posted right in the issue thread. Fix, push, repeat. When the code is clean, a draft PR opens automatically. It's a senior engineer reviewing your work asynchronously, before the PR ever exists.

## Concept

Named after the classic arcade game Frogger — your code navigates through lanes of traffic (review states) to reach a safe place (a clean draft PR). Each push is a hop. Each review is a lane. The goal is to get home safely.

## MVP Scope

### What froggr Does

1. Installs as a GitHub App on a repository
2. Detects branches linked to issues via naming convention (`42-add-auth` -> Issue #42)
3. On every push to a linked branch, reviews the diff against the default branch
4. Posts structured findings as an issue comment
5. On subsequent pushes, re-reviews with awareness of prior feedback to avoid repeating resolved items
6. When review is clean, opens a draft PR with `Closes #42`

### What froggr Does Not Do (Yet)

- Auto-fix code
- Custom rules or review profiles
- Notifications outside GitHub
- Monorepo-aware review scoping
- Self-hosted / on-prem deployment

### Configuration

```yaml
# .froggr.yml
branch_pattern: "^(\\d+)-"   # how to extract issue number from branch name
auto_draft_pr: true
ignore_paths:
  - "*.lock"
  - "vendor/**"
  - "generated/**"
provider: "openrouter"                # "openrouter" (default) or "bedrock"
model: "anthropic/claude-sonnet-4"    # any model ID on the chosen provider
```

Missing config falls back to defaults. Other config fetch failures do not.
If GitHub cannot read `.froggr.yml` because of auth, rate limits, or upstream
errors, froggr skips the review rather than silently changing the repo's
configured policy.

## Architecture

```
GitHub ──webhooks──> Webhook Server ──> Review Queue ──> Review Worker
                          |              (debounce)           |
                          |                                   |-- fetch diff via GitHub API
                          |                                   |-- build review context
                          |                                   |-- call AI provider (OpenRouter / Bedrock)
                          |                                   |-- format + post results
                          |                                   |
                          |--- GitHub API <--------------------+
                               (comments, draft PRs)
```

### Components

| Component | Responsibility |
|-----------|---------------|
| **Webhook Handler** | Receives GitHub events, validates HMAC signatures, routes to handlers |
| **Debounce Buffer** | Collapses rapid pushes (30s window) into a single review |
| **Review Worker** | Builds review context, calls AI provider (OpenRouter or Bedrock), parses response |
| **GitHub Client** | Posts issue comments, creates draft PRs, reads issues and file content |

### Webhook Events

| Event | Trigger | Action |
|-------|---------|--------|
| `push` | Branch push | If branch name matches an open issue, queue a review |
| `issues.closed` | Issue closed | Stop watching the linked branch |
| `installation.created` | App installed | Store installation credentials |

## Review Flow

```
1. Push event received for branch `42-add-auth`
2. Parse issue number from branch name -> 42
3. Verify issue #42 exists and is open
4. Debounce: if another push arrives within 30s, reset the timer
5. Fetch via GitHub API:
   a. Diff: default_branch...42-add-auth
   b. Full content of changed files (surrounding context)
   c. Issue title + body (developer's intent)
   d. Previous froggr comments on the issue (to track resolved items)
6. Build prompt with all context
7. Call AI provider (OpenRouter or Bedrock) with configured model
8. Parse response into structured findings
9. Post issue comment with findings
10. If no issues found AND auto_draft_pr is enabled:
    a. Create draft PR via GitHub API
       If the matching PR already exists, reuse it as the successful outcome
    b. Body includes `Closes #42`
    c. Post final comment on issue: "Draft PR opened: #XX"
```

## State Management — Stateless MVP

All state is derived from GitHub itself:

- **Issue open/closed** -> whether to watch the linked branch
- **Issue comments** -> review history and what's been resolved
- **Branch existence** -> whether there's active work

Server-side state (in-memory only):

- GitHub App installation tokens (short-lived, cached)
- Debounce timers (ephemeral)

**No database for MVP.** GitHub is the source of truth.

Operational safety rules:

- GitHub API clients use bounded HTTP timeouts
- Debounced review runs inherit a handler-owned context with a hard timeout
- Shutdown cancels in-flight reviews after the HTTP server stops accepting work

## AI Review Strategy

### Context Sent to the Model

| Context | Purpose |
|---------|---------|
| Issue title + body | The "why" — developer's intent |
| The diff | The "what" — what changed |
| Full file content for changed files | Surrounding context for the diff |
| Prior froggr comments | What was already flagged, what's been resolved |
| `.froggr.yml` ignore paths | What to skip |

froggr does not send an unbounded prompt. For speed and safety, review context
is budgeted before it reaches the model:

- At most 25 changed files are fetched for full review context
- At most the 5 most recent froggr reviews are included
- Large issue bodies, patches, file contents, and prior reviews are truncated
- The final prompt has a hard size cap, and omissions are called out explicitly

This keeps review latency and cost predictable and avoids depending on
provider-side truncation, which is harder to reason about and easy to miss.

froggr also treats GitHub's compare API limits as correctness boundaries. The
compare endpoint exposes at most 300 changed files for a comparison, so if a
branch hits that ceiling froggr refuses the review rather than overclaim that a
partial diff was fully analyzed. In that case froggr posts an issue comment
explaining that the change set must be split or narrowed before review can
continue safely.

### Review Focus

froggr focuses on things that matter:

- Bugs and logic errors
- Security issues
- Missing edge cases
- Incorrect error handling
- Race conditions and concurrency issues

froggr explicitly avoids:

- Style and formatting nitpicks (that's what linters are for)
- Naming suggestions (subjective, noisy)
- "Consider using X instead of Y" without a concrete reason

### Comment Format

```markdown
## froggr review — `42-add-auth` @ abc1234

**Bug** — `src/auth.go:45`
Token expiry uses `<` instead of `<=`, off-by-one allows expired tokens for 1 second.

**Concern** — `src/middleware.go:12`
Error from `validateToken()` is logged but the request continues. Should return 401.

**Looks good**
- No injection vectors in new query code
- Auth middleware applied to all protected routes

---
*Push fixes and I'll review again. When clean, I'll open a draft PR.*
```

froggr parses model output conservatively. A run is only considered clean when
the model returns an explicit empty JSON array. Malformed JSON, unsupported
severities, missing finding fields, or off-format prose fail the run instead of
being treated as a successful clean review.

### Resolved Issue Tracking

On subsequent reviews, froggr compares new findings against previous comments. If an issue was flagged in a prior review and no longer appears in the code, it's marked as resolved. This prevents noise from repeating fixed issues and gives the developer a clear sense of progress.

## AI Provider Integration

froggr supports multiple AI providers. Each repo selects its provider via the
`provider` field in `.froggr.yml`, or it is auto-detected from the model ID
format (slash = OpenRouter, dot = Bedrock). Provider-agnostic types live in
`internal/ai/`; provider-specific logic is encapsulated in `internal/openrouter/`
and `internal/bedrock/`.

### OpenRouter (default)

froggr uses [OpenRouter](https://openrouter.ai) as its default AI gateway. This gives users the freedom to choose any model — they're not locked into a single provider.

- **Model choice**: Claude, GPT-4, Gemini, Llama, Mistral, DeepSeek, and more
- **Single integration**: One SDK, one API key, access to all providers
- **Fallback routing**: OpenRouter can automatically fall back if a provider is down
- **Cost transparency**: Users see per-model pricing and choose their cost/quality tradeoff

```yaml
model: "anthropic/claude-sonnet-4"   # OpenRouter model IDs contain a slash
```

### AWS Bedrock

froggr also supports AWS Bedrock via the Converse API, using the standard AWS
credential chain. The Bedrock client separates system messages into Bedrock's
dedicated `System` field automatically.

```yaml
provider: bedrock
model: anthropic.claude-sonnet-4-6   # Bedrock model IDs contain a dot
```

### Configuration

Users set their preferred model in `.froggr.yml`. The API key (OpenRouter) or
AWS credentials (Bedrock) are configured at the server level via environment
variables.

## Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Language | Go | Small binary, fast startup, strong concurrency, good GitHub libraries |
| Hosting | Fly.io or Railway | Single container, no infra management |
| AI Providers | OpenRouter + AWS Bedrock | Model-agnostic via provider map; users choose per-repo |
| Queue | In-process goroutine + channel | No external dependencies for MVP |
| Database | None | Stateless — GitHub is the source of truth |
| Auth | GitHub App JWT -> installation tokens | Standard GitHub App authentication flow |

## Security

- **Webhook validation**: HMAC-SHA256 signature verification on every incoming request
- **Token scoping**: Installation tokens are limited to repos that installed the app
- **No code persistence**: Code is fetched via API, reviewed in memory, then discarded
- **Code sent to AI provider**: Same trust model as GitHub Copilot, Greptile, or CodeRabbit — users opt in by installing the app
- **Secrets protection**: Respects `.gitignore` and `ignore_paths` — never sends lockfiles, env files, or generated code to the model

## Post-MVP Roadmap

1. **Review profiles** — configurable review focus (security-heavy, performance, etc.)
2. **Suggested fixes** — AI proposes patches, developer applies with one click
3. **Analytics** — issues caught pre-PR vs post-PR, review cycle time
4. **Team notifications** — Slack/Discord alerts when draft PR opens
5. **Monorepo support** — scope reviews to changed packages only
6. **Self-hosted deployment** — for orgs that can't send code to external AI providers
7. **Branch protection integration** — froggr approval as a required check
8. **Multi-model review** — run multiple models and compare findings

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
model: "anthropic/claude-sonnet-4"  # any OpenRouter model ID
```

## Architecture

```
GitHub ──webhooks──> Webhook Server ──> Review Queue ──> Review Worker
                          |              (debounce)           |
                          |                                   |-- fetch diff via GitHub API
                          |                                   |-- build review context
                          |                                   |-- call OpenRouter API
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
| **Review Worker** | Builds review context, calls AI via OpenRouter, parses response |
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
7. Call OpenRouter API with configured model
8. Parse response into structured findings
9. Post issue comment with findings
10. If no issues found AND auto_draft_pr is enabled:
    a. Create draft PR via GitHub API
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

## AI Review Strategy

### Context Sent to the Model

| Context | Purpose |
|---------|---------|
| Issue title + body | The "why" — developer's intent |
| The diff | The "what" — what changed |
| Full file content for changed files | Surrounding context for the diff |
| Prior froggr comments | What was already flagged, what's been resolved |
| `.froggr.yml` ignore paths | What to skip |

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

### Resolved Issue Tracking

On subsequent reviews, froggr compares new findings against previous comments. If an issue was flagged in a prior review and no longer appears in the code, it's marked as resolved. This prevents noise from repeating fixed issues and gives the developer a clear sense of progress.

## OpenRouter Integration

froggr uses [OpenRouter](https://openrouter.ai) as its AI gateway. This gives users the freedom to choose any model — they're not locked into a single provider.

### Why OpenRouter

- **Model choice**: Claude, GPT-4, Gemini, Llama, Mistral, DeepSeek, and more
- **Single integration**: One SDK, one API key, access to all providers
- **Fallback routing**: OpenRouter can automatically fall back if a provider is down
- **Cost transparency**: Users see per-model pricing and choose their cost/quality tradeoff

### Configuration

Users set their preferred model in `.froggr.yml`:

```yaml
# Fast and cheap for most reviews
model: "anthropic/claude-sonnet-4"

# Thorough for security-critical repos
model: "anthropic/claude-opus-4"

# Budget-friendly alternative
model: "google/gemini-2.5-pro"
```

The OpenRouter API key is configured at the GitHub App level (managed by froggr's hosted service) or provided by the user for self-hosted deployments.

## Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Language | Go | Small binary, fast startup, strong concurrency, good GitHub libraries |
| Hosting | Fly.io or Railway | Single container, no infra management |
| AI Gateway | OpenRouter SDK | Model-agnostic, single integration point |
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

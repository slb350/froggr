# froggr

Navigate your code through traffic to a safe place.

**froggr** is a GitHub App that reviews your code *while you're still writing it* — not after you open a PR, when you've already context-switched to the next thing. Think of it like Frogger: your code hops through lanes of review (states) to safely reach the other side (a clean draft PR).

## How It Works

1. **Create a GitHub Issue** describing the work
2. **Push to a branch** matching the issue (`42-add-auth` links to Issue #42)
3. **froggr reviews your code** on every push and posts findings in the issue thread
4. **Fix, push, repeat** — froggr re-reviews, tracking what's been resolved
5. **When clean, froggr opens a draft PR** linked to the issue automatically

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
model: "anthropic/claude-sonnet-4"
```

froggr uses [OpenRouter](https://openrouter.ai) under the hood, so you can use any model — Claude, GPT-4, Gemini, Llama, Mistral, or whatever suits your codebase and budget.

## Installation

> Coming soon — froggr is in early development.

## Development

See [CLAUDE.md](./CLAUDE.md) for development guidelines and [docs/](./docs/) for architecture and design.

## License

MIT

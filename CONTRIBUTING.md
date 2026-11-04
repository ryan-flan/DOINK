# Contributing

Bug reports and requests: https://github.com/ryan-flan/DOINK/issues
(templates ask for the versions and logs that make a report actionable).

## Setup

Go, LuaJIT, gh and the linters come from [devbox](https://www.jetify.com/devbox):

```sh
devbox run test            # addon harness + companion tests
devbox run lint            # luacheck + gofmt/vet/golangci-lint
devbox run dry-run --once --config path/to/config.toml   # companion against a real DOINK.lua
```

Put `addon/DOINK` at `<WoW>/<flavor>/Interface/AddOns/DOINK` (a copy or a
link) to test in game; `/doink test <type>` emits a fake event through the
real pipeline, `/doink debug` logs every event the notifiers see.

## What to know before changing things

- The **data contract** (the JSON each event carries, in `CLAUDE.md`) is
  shared by the addon and the companion. Change it in both halves in one
  commit, with `!` and a `BREAKING CHANGE:` footer.
- The addon runs in WoW's sandbox: no network, no sockets, SavedVariables
  are written only on `/reload` or logout. Don't write addon code that
  assumes a particular transport to the companion.
- Design notes, verified WoW API facts and open questions live in
  `CLAUDE.md`. When you learn something about the Forever client's API,
  add it to the "Beta facts" section with the date.

## Commits

[Conventional Commits](https://www.conventionalcommits.org): `type(scope):
imperative summary`, with scopes `addon`, `companion` or `contract` (omit
for repo-wide changes). Summaries become release notes, so write them for
a player. One logical change per commit.

## Pull requests

CI runs the addon harness, luacheck, gofmt, vet, golangci-lint, the Go
tests with the race detector (Linux) and on Windows, and govulncheck.
Keep them green. Say what you tested in game, if anything.

# Contributing

Build and test commands are in [CLAUDE.md](CLAUDE.md). The v1 architecture is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md); future v2 work is in
[docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

Discuss substantial API or architecture changes in an issue before opening a PR.
Branch from `master`, with one topic per branch. Both modules target `master`:
v1 changes belong at the root, and v2 changes in `v2/`.

A PR describes the concrete problem, resulting behavior and validation. Include
a regression test for a bug, demonstrate that it fails without the fix, and use
`-race` for concurrency changes. Run the relevant tests and `make lint`; update
the owning documentation rather than repeating it across files. Report checks
that could not be run.

Review the final implementation and tests before merging. The owner merges PRs
unless they explicitly delegate it. Use squash merge by default. Do not rewrite
`master`, `v1.x` or another contributor's published branch. Commit messages and
PR text are English and have no generated footers or trailers.

V1 has no standalone changelog or completed-stage ledger. Git commits and PRs
record changes; release documentation describes the API, defaults, supported
behavior and limitations. Do not retain compatibility stubs solely for an
unreleased intermediate API. V2 changes follow its own roadmap and changelog.

## Releases

Only create a release or tag on the owner's explicit instruction, from `master`.
The first release of the fork is `v1.5.0`; v2 releases follow it. The frozen
`v1.x` branch is not a release target.

| Module | Tag |
| --- | --- |
| Root `github.com/sshaplygin/go-socket.io` | `v1.X.Y` |
| `github.com/sshaplygin/go-socket.io/v2` | `v2.X.Y` |
| Nested `github.com/sshaplygin/go-socket.io/v2/<dir>/vN` | `v2/<dir>/vN.X.Y` |

Before tagging v1:

1. Run the full CI matrix, including Go 1.22, race/stress tests, lint,
   vulnerability checks, architecture/consumer fixtures and coverage.
2. Build the examples and run Node interoperability with both pinned clients,
   Redis TCP/Unix examples and the mixed Go/Node Redis adapter.
3. Check that the usage and protocol documents agree with the implementation.
   Resolve release-blocking defects; do not replace them with completion claims.
4. Put the requested version in the README install command and pkg.go.dev links
   for the root and Engine.IO packages. Verify the module can be consumed under
   the fork's path. Create the tag on this verified release commit.

Installation examples must use a published fork version: upstream tags use a
different module path. Prepare versioned links in the release PR and publish the
tag with the release commit.
V1 needs no changelog preparation. For v2, its release commit also dates the
`Unreleased` section in `v2/CHANGELOG.md`.

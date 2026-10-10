EXAMPLES := $(patsubst %/go.mod,%,$(wildcard _examples/*/go.mod))
EXPERIMENTS := $(patsubst %/go.mod,%,$(wildcard _experiments/*/go.mod))

.PHONY: all
all:
	go install ./...

.PHONY: test
test:
	go test -count=1 ./...

.PHONY: test-race
test-race:
	go test -race -count=1 ./...

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: lint
lint:
	gofmt -s -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...
	golangci-lint run ./...

# The legacy _examples modules are not scanned: they are written against the v1 API, which
# the v2 root package does not have, and do not build until stage 2.5D migrates them
# (docs/ROADMAP.md, 2.0).
.PHONY: vuln
vuln:
	set -e; \
	govulncheck ./...; \
	for d in $(EXPERIMENTS); do \
		(cd "$$d" && govulncheck ./...); \
	done

.PHONY: cover
cover:
	go test -count=1 -coverprofile=c.out ./... && go tool cover -html=c.out

# The legacy application examples are written against the v1 API, which stage 2.0 removed
# from the v2 root package (v1 is the module at the repository root), so they are not built
# or tested until stage 2.5D migrates them. The check that
# every chat.go is identical to _examples/default-http/chat.go still runs, on the files
# only.
.PHONY: examples
examples:
	@set -e; for d in $(EXAMPLES); do cmp $$d/chat.go _examples/default-http/chat.go; done

# The package graph check of docs/API.md: no import cycle and the layering of the
# roadmap (the root never imports adapter/, adaptertest/, client/, contrib/ or
# internal/fixtures/; engineio, parser and logger never import the root).
.PHONY: graph
graph:
	go test -count=1 -run '^(TestPackageGraph|TestForbiddenEdge)$$' .

# The G2 check of docs/API.md (Frozen contract): no unresolved marker in docs/API.md or in
# the comments of the frozen declarations, and no bare any in an exported frozen signature, func type or struct field.
.PHONY: freeze
freeze:
	go test -count=1 -run '^(TestFrozenContract|TestAnyUsesDetects)$$' .

# The gate evidence of ROADMAP wave 2A: the compile fixtures, the package graph and the
# frozen-contract check. The inventory check keeps docs/API.md complete.
.PHONY: g2
g2: graph freeze
	go test -count=1 -run '^(TestCompileContracts|TestInventory.*)$$' .

# First the isolation check (the Stage 1b DoD line "_experiments stays standalone", see
# docs/ROADMAP.md): no root-module import of an _experiments package, no go.work, every
# Go file under a go.mod of its own. Then vet, gofmt, golangci-lint (root config) and race
# tests in every standalone _experiments module; stops at the first failure, prints
# nothing when there are none.
.PHONY: experiments
experiments:
	@deps="$$(go list -deps -test -f '{{.Dir}}' ./...)" || { echo "_experiments check: go list failed (an unresolvable import in the root module also fails here)" >&2; exit 1; }; \
	bad="$$(echo "$$deps" | grep '/_experiments/')$$(git ls-files go.work)$$(git ls-files '_experiments/*.go' | while read f; do d=$$(dirname $$f); until [ -e $$d/go.mod ] || [ $$d = _experiments ]; do d=$$(dirname $$d); done; [ -e $$d/go.mod ] || echo $$f; done)"; \
	test -z "$$bad" || { echo "_experiments must stay standalone: root import, go.work or Go file outside a module:" >&2; echo "$$bad" >&2; exit 1; }
	@set -e; for d in $(EXPERIMENTS); do \
		echo "==> $$d"; \
		(cd $$d && go vet ./... && test -z "$$(gofmt -s -l . | tee /dev/stderr)" \
			&& golangci-lint run --config "$(CURDIR)/.golangci.yml" ./... \
			&& go test -race -count=1 ./...); \
	done

EXAMPLES := $(patsubst %/go.mod,%,$(wildcard _examples/*/go.mod))
CHAT_SKIP := _examples/redis-adapter _examples/redis-adapter-unix-socket
CHAT_COPIES := $(filter-out $(CHAT_SKIP),$(patsubst %/chat.go,%,$(wildcard _examples/*/chat.go)))
LIFECYCLE_COPIES := $(wildcard _examples/*/lifecycle.go)

# Measured on V1-20; later PRs may raise the floor or lower the ceiling only.
COVER_FLOOR := 91.7
API_UNCOVERED_MAX := 0

.PHONY: all
all:
	go install ./...

.PHONY: test
test:
	go test -count=1 ./...

.PHONY: test-race
test-race:
	go test -race -count=1 ./...

# Optional interoperability gate; default Go tests do not require Node.
.PHONY: conformance
conformance:
	go vet -tags conformance ./internal/conformance/...
	go test -race -count=1 -tags conformance ./internal/conformance/...

# Race tests of the packages that run goroutines per connection, repeated and shuffled.
.PHONY: test-stress
test-stress:
	go test -race -count=5 -shuffle=on -cpu=1,4 . ./adapter/... ./internal/connection ./internal/namespace ./client/... ./parser ./engineio/...

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: lint
lint:
	gofmt -s -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./... ./.github/architecture
	golangci-lint run ./... ./.github/architecture

# The final Component boundaries graph is owned by docs/ROADMAP.md.
.PHONY: architecture-check
architecture-check:
	go test -race -count=1 ./.github/architecture
	go run ./.github/architecture

.PHONY: vuln
vuln:
	set -e; \
	govulncheck ./...; \
	for d in _examples/*/go.mod; do \
		(cd "$$(dirname "$$d")" && govulncheck ./...); \
	done

.PHONY: cover
cover:
	go test -count=1 -coverprofile=c.out ./... && go tool cover -html=c.out

.PHONY: parity-check
parity-check:
	go test -count=1 -run '^TestParityCovers$$' ./internal/parity

.PHONY: cover-check
cover-check:
	@set -eu; \
	profile=$$(mktemp); summary=$$(mktemp); annotated=$$(mktemp); \
	trap 'rm -f "$$profile" "$$summary" "$$annotated"' 0; \
	go test -count=1 -coverpkg=./... -coverprofile="$$profile" ./...; \
	go tool cover -func="$$profile" >"$$summary"; \
	go test -count=1 ./.github/coverage; \
	go run ./.github/coverage -profile "$$profile" -summary "$$summary" -root . -module "$$(go list -m)" >"$$annotated"; \
	awk -v floor="$(COVER_FLOOR)" -v max="$(API_UNCOVERED_MAX)" '\
		BEGIN { \
			if (floor !~ /^[0-9]+([.][0-9]+)?$$/ || floor+0 > 100 || max !~ /^[0-9]+$$/) { \
				print "cover-check: invalid COVER_FLOOR or API_UNCOVERED_MAX"; exit 1; \
			} \
		} \
		$$1 == "total:" { totals++; value=$$3; sub(/%$$/, "", value); total=value+0 } \
		$$3 == "0.0%" && $$2 ~ /^[A-Z]/ { uncovered++; print "uncovered API: " $$1 " " $$2 } \
		END { \
			if (totals != 1 || value !~ /^[0-9]+([.][0-9]+)?$$/) { print "cover-check: missing coverage total"; exit 1 } \
			printf "coverage: %.1f%% (floor %s%%); uncovered API: %d (maximum %s)\n", total, floor, uncovered, max; \
			if (total < floor+0 || uncovered > max+0) exit 1; \
		}' "$$annotated"

.PHONY: examples
examples:
	@set -e; for d in $(EXAMPLES); do echo "==> $$d"; (cd $$d && go build -o /dev/null ./... && go vet ./...); done
	go build -o /dev/null ./_examples/client
	go build -o /dev/null ./engineio/_examples
	go vet ./engineio/_examples
	go test -race -count=1 ./engineio/_examples
	@set -e; for d in $(CHAT_COPIES); do cmp $$d/chat.go _examples/default-http/chat.go; done
	@set -e; for f in $(LIFECYCLE_COPIES); do cmp $$f _examples/default-http/lifecycle.go; done
	cd _examples/default-http && go test -race -count=1 ./...

.PHONY: examples-node
# CONFORMANCE_NODE_MODULES selects the pinned socket.io-client used by the runner.
examples-node:
	bash _examples/run-node.sh

# Foundation files stay independent copies in both modules (ROADMAP D14).
.PHONY: sync-check
sync-check:
	@set -eu; \
	tmp=$$(mktemp); trap 'rm -f "$$tmp"' 0; \
	while read -r root_file mirror_file extra || test -n "$$root_file"; do \
		case "$$root_file" in ''|\#*) continue ;; esac; \
		if test -z "$$mirror_file" || test -n "$$extra"; then \
			echo "sync-check: expected two paths: $$root_file $$mirror_file $$extra" >&2; exit 1; \
		fi; \
		sed 's#github[.]com/sshaplygin/go-socket[.]io/v2#github.com/sshaplygin/go-socket.io#g' "$$mirror_file" > "$$tmp"; \
		cmp "$$root_file" "$$tmp" || { \
			echo "sync-check: shared files differ: $$root_file $$mirror_file" >&2; exit 1; \
		}; \
	done < .github/sync-manifest

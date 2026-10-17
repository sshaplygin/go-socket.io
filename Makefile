EXAMPLES := $(patsubst %/go.mod,%,$(wildcard _examples/*/go.mod))

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
	go test -race -count=5 -shuffle=on -cpu=1,4 . ./parser ./engineio/...

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: lint
lint:
	gofmt -s -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...
	golangci-lint run ./...

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

.PHONY: examples
examples:
	@set -e; for d in $(EXAMPLES); do echo "==> $$d"; (cd $$d && go build -o /dev/null ./...); done
	go build -o /dev/null ./_examples/client
	@set -e; for d in $(EXAMPLES); do cmp $$d/chat.go _examples/default-http/chat.go; done
	cd _examples/default-http && go test -race -count=1 ./...

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

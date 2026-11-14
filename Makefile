.PHONY: build image test cli-test web browser-test cli-readme clean lint format format-check ci-test release-binaries
export GOCACHE ?= /tmp/hoshilet-go-cache
web:
	pnpm --dir web install --frozen-lockfile
	pnpm --dir web build
lint:
	cd cli/python && uv run ruff check
	cd cli/python && uv run pyrefly check
	pnpm --dir cli/npm lint
	pnpm --dir web lint
format:
	cd cli/python && uv run ruff format
	pnpm --dir cli/npm format
	pnpm --dir web format
format-check:
	cd cli/python && uv run ruff format --check
	pnpm --dir cli/npm format:check
	pnpm --dir web format:check
ci-test: test cli-test browser-test
build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/hoshilet-server ./cmd/hoshilet-server
release-binaries:
	mkdir -p dist/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/server/hoshilet-server-linux-amd64 ./cmd/hoshilet-server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/server/hoshilet-server-linux-arm64 ./cmd/hoshilet-server
image:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/hoshilet-server ./cmd/hoshilet-server
	podman build -t localhost/hoshilet:0.1.0 -f Containerfile .
test:
	go test ./...
	pnpm --dir cli/npm test
	uv run --project cli/python python -m unittest discover -s cli/python/tests
cli-test:
	pnpm --dir cli/npm build
	HOSHILET_TEST_CLI=1 go test ./internal/platform -run 'TestCLI(Contract|OIDC)|TestSlowHTTPUploadDeadline|TestCLIServerMessages' -count=1
browser-test: web
	HOSHILET_TEST_BROWSER=1 go test ./internal/platform -run TestBrowser -count=1
cli-readme:
	node scripts/gen-cli-readme.mjs npm
	node scripts/gen-cli-readme.mjs python
clean:
	rm -rf bin cli/npm/README.md cli/python/README.md

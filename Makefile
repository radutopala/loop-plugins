# Checks, builds and installs one plugin, cogtest by default:
#
#   make check                 # go vet/test, golangci-lint and the web checks
#   make install               # build the image and install it as a dev plugin
#   make install PLUGIN=foo    # plugins/foo
#   make check DIR=template    # any plugin dir
#
# A panel on @radutopala/plugin-ui installs it from GitHub Packages with the
# token in NODE_AUTH_TOKEN, the GitHub CLI's by default; it needs the
# read:packages scope (gh auth refresh -s read:packages).

PLUGIN ?= cogtest
DIR ?= plugins/$(PLUGIN)
NODE_AUTH_TOKEN ?= $(shell gh auth token 2>/dev/null)
export NODE_AUTH_TOKEN
LOOP_BIN ?= loop
GOLANGCI_LINT_VERSION ?= v2.14.0
# The tag `loop plugin:dev` would give the dir.
TAG ?= loop-plugin-dev/$(notdir $(DIR)):latest

.PHONY: check test lint web build install

check: test lint web

test:
	cd $(DIR) && go vet ./... && go test -race ./...

lint:
	docker run --rm -v "$(abspath $(DIR))":/src -w /src \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run --build-tags ui ./...

web:
	@if [ -f $(DIR)/web/package-lock.json ]; then \
		cd $(DIR)/web && npm ci && npm run --if-present lint && \
		npm run --if-present typecheck && npm run --if-present test && npm run build; \
	fi

build:
	docker build \
		--secret id=NODE_AUTH_TOKEN,env=NODE_AUTH_TOKEN \
		-t $(TAG) $(DIR)

install: build
	$(LOOP_BIN) plugin:install --dev --yes $(TAG)

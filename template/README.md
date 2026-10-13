# Plugin template

A small working plugin to copy when starting a new one. It has every part a
plugin usually needs:

- `plugin.json`: the manifest, with a tool (`count_lines`), an action
  (`count`), a per-channel setting (`glob`) and a panel (`main`).
- `main.go`: reads the environment Loop sets, opens the store under the data
  dir and serves the contract with `pluginsdk.Server`.
- `internal/server`: the action, the tool and the panel's `/x` API, behind a
  narrow `Loop` interface so tests can mock it.
- `internal/store`: state in one JSON file under `/data`, written atomically.
- `web/`: the panel, a React app built with Vite on `@radutopala/plugin-ui`.
- `Dockerfile`: a multi-arch build on a distroless, non-root base.
- Tests: unit tests with testify, `main_test.go` runs the SDK's conformance
  check against a fake Loop, and `ui_test.go` (build tag `ui`) loads the
  built panel in headless Chrome under Loop's content security policy.

The Go SDK, `github.com/radutopala/loop/pluginsdk`, is a regular module
dependency. The panel SDK, `@radutopala/plugin-ui`, is on GitHub Packages;
`web/.npmrc` points its scope there and reads the token from
`NODE_AUTH_TOKEN`, a GitHub token with `read:packages`.

## Starting a plugin

```sh
template/new.sh my-plugin
```

copies the template to `plugins/my-plugin` and renames it: the manifest id,
the Go module path and the image name. Then edit `plugin.json` (name,
description, capabilities, tools, actions, settings, panels), replace the
example code, and add the plugin to the table in the root README.

## Building and testing

From `plugins/<id>` or `template/`:

```sh
export NODE_AUTH_TOKEN=$(gh auth token)   # with read:packages
go test -race ./...
(cd web && npm ci && npm run lint && npm run typecheck && npm test && npm run build)
go test -tags ui -run UI ./...   # needs web/dist and Chrome
docker run --rm -v "$PWD":/src -w /src \
  golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags ui ./...
docker build --secret id=NODE_AUTH_TOKEN,env=NODE_AUTH_TOKEN \
  -t loop-plugin-dev/<id>:latest .
loop plugin:install --dev --yes loop-plugin-dev/<id>:latest
```

## Panel rules

Loop serves a panel's files under a strict content security policy: scripts
and styles from the plugin's own bundle only, no inline scripts, no remote
URLs, no `eval`. The panel talks to Loop and to its plugin only through
`@radutopala/plugin-ui` (`connect`, `api`, `createStorage`). Biome
(`web/biome.json`, with the remote-URL check in `web/panel-rules.grit`)
flags remote URLs, `eval`-like calls, `fetch` and browser storage, and
`ui_test.go` catches what slips past it.

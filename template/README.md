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
- `web/`: the panel, a React app built with Vite on `@loop/plugin-ui`.
- `Dockerfile`: a multi-arch build on a distroless, non-root base.
- Tests: unit tests with testify, `main_test.go` runs the SDK's conformance
  check against a fake Loop, and `ui_test.go` (build tag `ui`) loads the
  built panel in headless Chrome under Loop's content security policy.

Like the plugins, it builds against a Loop checkout next to this repo
(`../loop`): `go.mod` replaces `pluginsdk` with it, and `web/package.json`
installs `@loop/plugin-ui` from it.

## Starting a plugin

```sh
template/new.sh my-plugin
```

copies the template to `plugins/my-plugin` and renames it: the manifest id,
the Go module path, the image name, and the relative paths to the Loop
checkout, which are one dir deeper there. Then edit `plugin.json` (name,
description, capabilities, tools, actions, settings, panels), replace the
example code, and add the plugin to the table in the root README.

## Building and testing

From `plugins/<id>` (from `template/`, drop one `../` from each Loop path):

```sh
go test -race ./...
(cd web && npm ci && npm run lint && npm run typecheck && npm test && npm run build)
go test -tags ui -run UI ./...   # needs web/dist and Chrome
docker run --rm -v "$PWD":/src -v "$PWD/../../../loop/pluginsdk":/loop/pluginsdk -w /src \
  golangci/golangci-lint:v2.14.0 golangci-lint run --build-tags ui ./...
docker build \
  --build-context pluginsdk=../../../loop/pluginsdk \
  --build-context plugin-ui=../../../loop/app/packages/plugin-ui \
  -t loop-plugin-dev/<id>:latest .
loop plugin:install --dev --yes loop-plugin-dev/<id>:latest
```

## Panel rules

Loop serves a panel's files under a strict content security policy: scripts
and styles from the plugin's own bundle only, no inline scripts, no remote
URLs, no `eval`. The panel talks to Loop and to its plugin only through
`@loop/plugin-ui` (`connect`, `api`, `createStorage`). `web/eslint.config.js`
flags remote URLs, `eval`-like calls, `fetch` and browser storage, and
`ui_test.go` catches what slips past it.

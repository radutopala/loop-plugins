# loop-plugins

Plugins for [Loop](https://github.com/radutopala/loop). Each plugin lives in
`plugins/<id>/` and is a Docker image that follows Loop's plugin contract
(`docs/plugins.md` in the Loop repo): a manifest at `/loop-plugin/plugin.json`,
an HTTP server on `$LOOP_PLUGIN_PORT`, and optional UI panels under
`/loop-plugin/ui/`.

## Plugins

| Plugin | What it does |
| --- | --- |
| [`cogtest`](plugins/cogtest) | A daily multiple-choice quiz about the channel's code. A read-only agent writes questions anchored in real lines, a panel asks them, per-area mastery is tracked with Leitner boxes, and missed questions come back while their code is unchanged. Each project gets a daily task for it, off until turned on in Tasks. |

## Working on a plugin

Plugins use the Go SDK in the Loop repo (`pluginsdk/`). Until it's published,
`go.mod` replaces it with a Loop checkout next to this repo
(`../loop/pluginsdk`), and the Docker build takes it as a named build context.

```sh
cd plugins/cogtest
go test ./...
(cd web && npm ci && npm run typecheck && npm test && npm run lint)
docker run --rm -v "$PWD":/src -v "$PWD/../../../loop/pluginsdk":/loop/pluginsdk -w /src \
  golangci/golangci-lint:v2.14.0 golangci-lint run ./...
docker build --build-context pluginsdk=../../../loop/pluginsdk -t loop-plugin-dev/cogtest:latest .
```

cogtest's panel is a React app in `web/`. `npm run build` bundles it into
`web/dist/`, and the Dockerfile builds it in a Node stage and copies it to
`/loop-plugin/ui/`.

`main_test.go` runs the SDK's conformance check against a fake Loop.

Install a local build with `loop plugin:install --dev --yes loop-plugin-dev/cogtest:latest`.

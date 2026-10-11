# loop-plugins

Plugins for [Loop](https://github.com/radutopala/loop). Each plugin lives in
`plugins/<id>/` and is a Docker image that follows Loop's plugin contract
(`docs/plugins.md` in the Loop repo): a manifest at `/loop-plugin/plugin.json`,
an HTTP server on `$LOOP_PLUGIN_PORT`, and optional UI panels under
`/loop-plugin/ui/`.

## Plugins

| Plugin | What it does |
| --- | --- |
| [`cogtest`](plugins/cogtest) | A daily multiple-choice quiz about the channel's code. A read-only agent writes questions anchored in real lines, a panel asks them, per-area mastery is tracked with Leitner boxes, and missed questions come back while their code is unchanged. |

## Working on a plugin

```sh
cd plugins/cogtest
go test ./...
(cd web && npm ci && npm run typecheck && npm test && npm run lint)
docker run --rm -v "$PWD":/src -w /src golangci/golangci-lint:v2.14.0 golangci-lint run ./...
docker build -t loop-plugin-dev/cogtest:latest .
```

cogtest's panel is a React app in `web/`. `npm run build` bundles it into
`web/dist/`, and the Dockerfile builds it in a Node stage and copies it to
`/loop-plugin/ui/`.

Install a local build with `loop plugin dev plugins/cogtest`.

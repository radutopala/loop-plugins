# loop-plugins

Plugins for [Loop](https://github.com/radutopala/loop). Each plugin lives in
`plugins/<id>/` and is a Docker image that follows Loop's plugin contract
(`docs/plugins.md` in the Loop repo): a manifest at `/loop-plugin/plugin.json`,
an HTTP server on `$LOOP_PLUGIN_PORT`, and optional UI panels under
`/loop-plugin/ui/`.

## Plugins

| Plugin | What it does |
| --- | --- |
| [`cogtest`](plugins/cogtest) | A daily multiple-choice quiz about the channel's code. A read-only agent writes questions anchored in real lines, a panel asks them, per-area mastery is tracked with Leitner boxes, and missed questions come back while their code is unchanged. A project gets a daily task for it once Settings → Plugins turns its schedule on. |

## Working on a plugin

Plugins use Loop's Go SDK, the `github.com/radutopala/loop/pluginsdk` module,
as a regular dependency; `go get github.com/radutopala/loop/pluginsdk@<commit or tag>`
moves a plugin to a newer one.

```sh
make check                 # go vet/test, golangci-lint and the web checks
make install               # build the image and install it as a dev plugin
```

or by hand:

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

`main_test.go` runs the SDK's conformance check against a fake Loop.

Install a local build with `loop plugin:install --dev --yes loop-plugin-dev/cogtest:latest`.

## Adding a plugin

`template/` is a small working plugin (Go server, store, React panel,
Dockerfile, tests) to start from. `template/new.sh <id>` copies it to
`plugins/<id>` and renames it; see [template/README.md](template/README.md).

## CI

`.github/workflows/plugins.yml` runs on pull requests and pushes to `main`
for every `plugins/<id>/` and `template/`: `go vet`, `go test -race`,
golangci-lint (when the dir has a `.golangci.yml`), the panel's `npm ci`,
lint, typecheck, tests and build, the UI conformance tests (`-tags ui`, in
headless Chrome), and a `linux/amd64` + `linux/arm64` image build. It checks
out Loop next to this repo so a panel's `file:` dependency on
`@loop/plugin-ui` resolves; set the repository variable `LOOP_REF` to build
against a Loop branch or tag other than `main`.

To publish a plugin, bump `version` in its `plugin.json`, then push a tag
`<id>/v<version>`:

```sh
git tag cogtest/v0.6.0 && git push origin cogtest/v0.6.0
```

The tag runs the same checks for that plugin only, fails if the tag doesn't
match the manifest's id and version or that version is already published,
and pushes `ghcr.io/radutopala/loop-plugin-<id>:<version>` and `:latest`
with the workflow's `GITHUB_TOKEN`. The template is never published.

Images are signed by digest with cosign when these repository secrets are
set; without them the image is pushed unsigned and the run notes it:

- `COSIGN_PRIVATE_KEY`: a cosign private key (`cosign generate-key-pair`).
- `COSIGN_PASSWORD`: its password.

Verify a published image with
`cosign verify --key cosign.pub ghcr.io/radutopala/loop-plugin-<id>:<version>`.

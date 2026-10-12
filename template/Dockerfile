# Build with Loop's SDKs as named contexts, from a Loop checkout:
#   docker build \
#     --build-context pluginsdk=<loop>/pluginsdk \
#     --build-context plugin-ui=<loop>/app/packages/plugin-ui \
#     -t loop-plugin-dev/example:latest .
# The stages lay the sources out as the repos are on disk, under /ws, so the
# relative paths in go.mod and web/package.json reach the SDKs.

# The UI and the binary build on the build machine's platform: the UI is
# the same for every platform, and Go cross-compiles.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
COPY --from=plugin-ui package.json /ws/loop/app/packages/plugin-ui/package.json
COPY --from=plugin-ui src /ws/loop/app/packages/plugin-ui/src
WORKDIR /ws/loop-plugins/template/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
COPY --from=pluginsdk . /ws/loop/pluginsdk
WORKDIR /ws/loop-plugins/template
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /plugin . && mkdir /data

# Loop's installer needs the label, a numeric non-root USER and a /data dir
# that USER owns.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL dev.loop.plugin=1
COPY --from=build /plugin /plugin
COPY --from=build --chown=65532:65532 /data /data
COPY plugin.json /loop-plugin/plugin.json
COPY --from=web /ws/loop-plugins/template/web/dist/ /loop-plugin/ui/main/
USER 65532:65532
ENTRYPOINT ["/plugin"]

FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /cogtest . && mkdir /data

FROM scratch
LABEL dev.loop.plugin=1
COPY --from=build /cogtest /cogtest
COPY --from=build --chown=65532:65532 /data /data
COPY plugin.json /loop-plugin/plugin.json
COPY --from=web /web/dist/ /loop-plugin/ui/
USER 65532:65532
ENTRYPOINT ["/cogtest"]

#!/bin/sh
# Copies the template to plugins/<id> and renames it: the manifest's id, the
# Go module path, the image name, and the relative paths to the Loop
# checkout, which sit one dir deeper there.
set -eu

id=${1:-}
if ! printf '%s' "$id" | grep -Eq '^[a-z][a-z0-9-]{1,31}$'; then
  echo "usage: $0 <id>   (id matches [a-z][a-z0-9-]{1,31})" >&2
  exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd)
dest="$root/plugins/$id"
if [ -e "$dest" ]; then
  echo "$dest already exists" >&2
  exit 1
fi

mkdir -p "$root/plugins"
(cd "$root/template" && tar -cf - --exclude node_modules --exclude dist --exclude README.md --exclude new.sh .) | (mkdir "$dest" && cd "$dest" && tar -xf -)

cd "$dest"
edit() { # edit <sed expression> <file>...: sed -i that works on GNU and BSD sed
  expr=$1
  shift
  for f in "$@"; do
    sed -e "$expr" "$f" >"$f.tmp" && mv "$f.tmp" "$f"
  done
}
edit "s#\"id\": \"example\"#\"id\": \"$id\"#" plugin.json
edit "s#loop-plugins/template#loop-plugins/plugins/$id#g" go.mod Dockerfile $(find . -name '*.go')
edit "s#=> ../../loop/pluginsdk#=> ../../../loop/pluginsdk#" go.mod
edit "s#loop-plugin-dev/example#loop-plugin-dev/$id#" Dockerfile
edit "s#\"example #\"$id #" main.go
edit "s#file:../../../loop/#file:../../../../loop/#; s#\"example-web\"#\"$id-web\"#" web/package.json
edit "s#\"../../../loop/#\"../../../../loop/#g; s#\"example-web\"#\"$id-web\"#" web/package-lock.json

echo "Created plugins/$id. Next: edit plugin.json (name, capabilities, tools, panels), then see template/README.md."

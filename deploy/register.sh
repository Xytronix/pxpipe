#!/bin/sh
set -eu

ROOT="${1:-.}"
if [ -f "$ROOT/transports/bifrost-http/server/plugins.go" ]; then
  MOD_DIR="$ROOT/transports"
elif [ -f "$ROOT/bifrost-http/server/plugins.go" ]; then
  MOD_DIR="$ROOT"
else
  echo "register.sh: could not find bifrost-http/server/plugins.go under $ROOT" >&2
  exit 1
fi
PLUGINS_GO="$MOD_DIR/bifrost-http/server/plugins.go"
PKG="github.com/maximhq/bifrost/plugins/pxpipe"

if ! grep -q "case pxpipe.PluginName:" "$PLUGINS_GO"; then
  awk '
    { print }
    /^\t"github.com\/maximhq\/bifrost\/plugins\/prompts"$/ {
      print "\t\"github.com/maximhq/bifrost/plugins/pxpipe\""
    }
    /return modelcatalogresolver\.Init\(bifrostConfig\.ModelCatalog, logger\)/ {
      print ""
      print "\tcase pxpipe.PluginName:"
      print "\t\tpxpipeConfig, err := MarshalPluginConfig[pxpipe.Config](pluginConfig)"
      print "\t\tif err != nil {"
      print "\t\t\treturn nil, fmt.Errorf(\"failed to marshal pxpipe plugin config: %w\", err)"
      print "\t\t}"
      print "\t\treturn pxpipe.Init(*pxpipeConfig, logger)"
    }
  ' "$PLUGINS_GO" > "$PLUGINS_GO.tmp" && mv "$PLUGINS_GO.tmp" "$PLUGINS_GO"
  gofmt -w "$PLUGINS_GO"
fi

( cd "$MOD_DIR"
  grep -q "plugins/pxpipe v" go.mod || go mod edit -require="$PKG@v0.1.0"
  grep -q "plugins/pxpipe =>" go.mod || go mod edit -replace="$PKG@v0.1.0=../plugins/pxpipe"
  go mod tidy )

echo "pxpipe registered into $MOD_DIR (compiled-in)"

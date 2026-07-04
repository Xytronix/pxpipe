.PHONY: plugin test clean

# Build pxpipe as a Bifrost shared-object plugin. Load it via a `path` entry in
# the gateway config — no edits to Bifrost source.
# Version-locked: build from the same Bifrost revision/Go toolchain as the gateway.
plugin:
	@mkdir -p build
	@go build -buildmode=plugin -o build/pxpipe.so ./cmd/plugin
	@echo "Built build/pxpipe.so"

test:
	@go test -count=1 ./...

clean:
	@rm -rf build

PREFIX  ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN     := bin/wt

.PHONY: build test vet install uninstall clean

build:
	@go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/wt
	@echo "Built: $(BIN) ($(VERSION))"

test:
	go test ./...

vet:
	go vet ./...

install: build
	@mkdir -p "$(PREFIX)"
	@install -m 0755 $(BIN) "$(PREFIX)/wt"
	@echo "Installed: $(PREFIX)/wt"
	@case ":$$PATH:" in \
		*":$(PREFIX):"*) ;; \
		*) echo "Warning: $(PREFIX) is not in your PATH. Add it in your shell profile:"; \
		   echo "  export PATH=\"$(PREFIX):\$$PATH\"" ;; \
	esac
	@echo "Enable 'wt cd' and completion by adding to your shell profile:"
	@echo "  eval \"\$$(wt shell-init zsh)\"   # or bash; fish: wt shell-init fish | source"

uninstall:
	@rm -f "$(PREFIX)/wt"
	@echo "Removed: $(PREFIX)/wt"

clean:
	@rm -rf bin

PREFIX ?= $(HOME)/.local/bin

.PHONY: install uninstall

install:
	@mkdir -p "$(PREFIX)"
	@cp wt "$(PREFIX)/wt"
	@chmod +x "$(PREFIX)/wt"
	@echo "Installed: $(PREFIX)/wt"
	@case ":$$PATH:" in \
		*":$(PREFIX):"*) ;; \
		*) echo "Warning: $(PREFIX) is not in your PATH. Add it in your shell profile:"; \
		   echo "  export PATH=\"$(PREFIX):\$$PATH\"" ;; \
	esac

uninstall:
	@rm -f "$(PREFIX)/wt"
	@echo "Removed: $(PREFIX)/wt"

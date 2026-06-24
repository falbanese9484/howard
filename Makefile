# howard — a minimal local AI agent

BINARY := howard
INSTALL_DIR := $(HOME)/.config/howard/bin

# Default target
.PHONY: build
build:
	go build -o $(BINARY) ./cmd/howard

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: install
install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	chmod +x $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "Make sure $(INSTALL_DIR) is in your PATH"

.PHONY: clean
clean:
	rm -f $(BINARY)

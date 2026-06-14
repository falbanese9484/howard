#!/bin/bash

set -e  # Exit on error

INSTALL_DIR="$HOME/.config/howard/bin"

echo "Creating Directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

echo "Building Binary..."
go build -o "$INSTALL_DIR/howard" ./cmd/howard/main.go

echo "Making Binary Executable..."
chmod +x "$INSTALL_DIR/howard"

echo "Binary moved to: $INSTALL_DIR"

# Add to PATH if not already added
if [[ ":$PATH:" !=  *":$HOME/.config/howard/bin:"* ]]; then
    echo 'export PATH="$HOME/.config/howard/bin:$PATH"' >> ~/.bashrc
    [ -f ~/.zshrc ] && \
    echo 'export PATH="$HOME/.config/howard/bin:$PATH"' >> ~/.zshrc
    echo "Added $INSTALL_DIR to PATH. Restart your terminal or run:"
    echo "source ~/.bashrc  (or source ~/.zshrc for zsh users)"
else
    echo "$INSTALL_DIR is already in PATH."
fi

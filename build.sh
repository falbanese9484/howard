#!/bin/bash

set -e  # Exit on error

# ---------------------------------------------------------------------------
# spiral_h: draw a block-letter "H" by spiraling characters in from the
# outside with a 256-color fade. Pure bash 3.2 safe (macOS /bin/bash).
# ---------------------------------------------------------------------------
spiral_h() {
    # 7 rows x 11 cols block "H". '#' = filled cell, ' ' = empty.
    local rows=(
        "##       ##"
        "##       ##"
        "##       ##"
        "###########"
        "##       ##"
        "##       ##"
        "##       ##"
    )
    local height=${#rows[@]}
    local width=11

    # Collect filled cells as "row,col" tokens.
    local cells=""
    local r c line ch
    for ((r = 0; r < height; r++)); do
        line="${rows[$r]}"
        for ((c = 0; c < width; c++)); do
            ch="${line:$c:1}"
            [ "$ch" = "#" ] && cells="$cells $r,$c"
        done
    done

    # Order cells by distance from center (outer -> inner) then by angle, so
    # they appear to swirl inward. Decorate-sort-undecorate via sort(1).
    local key_list=""
    local tok rr cc dr dc dist ang
    local cy=$(( (height - 1) * 5 ))   # center*10 to avoid floats
    local cx=$(( (width - 1) * 5 ))
    for tok in $cells; do
        rr="${tok%%,*}"
        cc="${tok##*,}"
        dr=$(( rr * 10 - cy ))
        dc=$(( cc * 10 - cx ))
        dist=$(( dr * dr + dc * dc ))          # squared radius (outer first)
        # crude angle bucket so cells at same radius sweep around
        ang=$(awk -v y="$dr" -v x="$dc" 'BEGIN{a=atan2(y,x); if(a<0)a+=6.2831853; printf "%05d", a*1000}')
        # negative dist for descending (outer rings first)
        key_list="$key_list$(printf '%08d %s %s\n' $((9999999 - dist)) "$ang" "$tok")
"
    done
    local ordered
    ordered=$(printf '%s' "$key_list" | sort -n | awk '{print $3}')

    # Paint.
    local lead=51    # bright cyan leading edge
    local body=39    # settled blue
    clear
    local prev=""
    for tok in $ordered; do
        rr="${tok%%,*}"
        cc="${tok##*,}"
        # settle the previous leading cell to body color
        if [ -n "$prev" ]; then
            local pr="${prev%%,*}"
            local pc="${prev##*,}"
            tput cup "$pr" "$pc"
            printf '\033[38;5;%dm#' "$body"
        fi
        tput cup "$rr" "$cc"
        printf '\033[38;5;%dm#' "$lead"
        prev="$tok"
        sleep 0.05
    done
    # settle final leading cell
    if [ -n "$prev" ]; then
        tput cup "${prev%%,*}" "${prev##*,}"
        printf '\033[38;5;%dm#' "$body"
    fi
    printf '\033[0m'
    tput cup "$height" 0
    echo
    printf '\033[38;5;%dm  a minimal local AI agent\033[0m\n' "$body"
    echo
}

if [ -t 1 ]; then
    spiral_h
else
    echo "==== howard ===="
    echo "a minimal local AI agent"
fi

INSTALL_DIR="$HOME/.config/howard/bin"

echo "Creating Directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

echo "Building Binary..."
go build -o "$INSTALL_DIR/howard" ./cmd/howard/main.go

echo "Making Binary Executable..."
chmod +x "$INSTALL_DIR/howard"

echo "Binary moved to: $INSTALL_DIR"

# Add to PATH if not already added
if [[ ":$PATH:" == *":$INSTALL_DIR:"* ]]; then
    echo "$INSTALL_DIR is already in PATH."
else
    for rc in ~/.bashrc ~/.zshrc; do
        [ -f "$rc" ] && echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$rc"
    done
    echo "Added $INSTALL_DIR to PATH. Restart your terminal or run:"
    echo "  source ~/.bashrc   (or ~/.zshrc for zsh)"
fi

#!/usr/bin/env bash
# a comment
set -euo pipefail

NAME="pier"
COUNT=42
RATIO=1.5e-3

greet() {
  local name="$1"
  echo "hello, ${name}!" "$HOME"
  printf '%s\n' "${name:-world}"
}

cat <<HEREDOC
heredoc ${NAME} $COUNT
HEREDOC

value=$((COUNT + 1))
if [ -n "$NAME" ]; then
  greet "$NAME"
fi

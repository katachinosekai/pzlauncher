#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
out=cmd/pzlauncher/THIRD_PARTY_LICENSES.txt
{
  echo "PZ Launcher includes the following third-party Go modules."
  echo "Each module's licence is reproduced in full below."
  echo
  go list -deps -tags wayland -f '{{if not .Standard}}{{with .Module}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/pzlauncher \
    | sort -u | grep -v '^github.com/katachinosekai/' | while read -r path ver dir; do
      lic=$(find "$dir" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'unlicense*' \) | sort | head -1)
      [ -n "$lic" ] || { echo "no licence file for $path" >&2; exit 1; }
      echo "================================================================================"
      echo "$path $ver"
      echo "================================================================================"
      echo
      cat "$lic"
      for n in $(find "$dir" -maxdepth 1 -type f -iname 'notice*' | sort); do echo; cat "$n"; done
      echo
    done
} > "$out"
echo "wrote $out"

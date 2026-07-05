#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

module_path=$(go list -m)
nonstdlib=$(
	go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' . |
		awk -v module="$module_path" '$0 != "" && $0 != module { print }'
)

if [ -n "$nonstdlib" ]; then
	echo "FAIL: root package imports non-stdlib packages:"
	echo "$nonstdlib"
	exit 1
fi

echo "OK: root package imports are stdlib-only."

#!/usr/bin/env bash
# Resolve direct dependency updates before changing the module files.
set -euo pipefail

query="${DEPS_UPDATE_QUERY:-patch}"

case "$query" in
patch | latest) ;;
*)
	echo "unsupported dependency update query: $query" >&2
	exit 2
	;;
esac

modules="$(
	go list -m -f \
		'{{ if and (not .Main) (not .Indirect) }}{{ .Path }} {{ .Version }}{{ end }}' \
		all
)"

updates=()
while read -r module current; do
	if [[ -z "$module" || -z "$current" ]]; then
		continue
	fi
	update=""
	case "$query" in
	patch)
		current_core="${current%%-*}"
		if [[ ! "$current_core" =~ ^(v[0-9]+\.[0-9]+\.)[0-9]+$ ]]; then
			continue
		fi
		patch_prefix="${BASH_REMATCH[1]}"
		versions="$(
			go list -m -versions -f \
				'{{ range .Versions }}{{ . }}{{ "\n" }}{{ end }}' \
				"$module"
		)"

		while read -r version; do
			version_core="${version%%-*}"
			if [[ "$version_core" == "$patch_prefix"* ]]; then
				update="$version"
			fi
		done <<< "$versions"
		;;
	latest)
		update="$(
			go list -m -u -f \
				'{{ if .Update }}{{ .Update.Version }}{{ end }}' \
				"$module"
		)"
		;;
	esac

	if [[ -n "$update" && "$update" != "$current" ]]; then
		updates+=("${module}@${update}")
	fi
done <<< "$modules"

if ((${#updates[@]} > 0)); then
	go get "${updates[@]}"
fi

go mod tidy

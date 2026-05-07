#!/usr/bin/env bash

slack_git_value() {
	local fallback="$1"
	shift

	git "$@" 2>/dev/null || printf '%s\n' "$fallback"
}

slack_version() {
	slack_git_value dev describe --tags --always --dirty
}

slack_commit() {
	slack_git_value unknown rev-parse --short HEAD
}

slack_branch() {
	slack_git_value unknown rev-parse --abbrev-ref HEAD
}

slack_build_time() {
	date -u +%Y-%m-%dT%H:%M:%SZ
}

slack_build_by() {
	whoami
}

slack_ldflags() {
	local module="${MODULE:?MODULE is required}"

	printf "%s" "-s -w"
	printf " -X '%s/internal/version.Version=%s'" "$module" "$(slack_version)"
	printf " -X '%s/internal/version.Commit=%s'" "$module" "$(slack_commit)"
	printf " -X '%s/internal/version.Branch=%s'" "$module" "$(slack_branch)"
	printf " -X '%s/internal/version.BuildTime=%s'" "$module" "$(slack_build_time)"
	printf " -X '%s/internal/version.BuildBy=%s'" "$module" "$(slack_build_by)"
}

slack_binary_path() {
	local dist_dir="${DIST_DIR:?DIST_DIR is required}"
	local binary_name="${BINARY_NAME:?BINARY_NAME is required}"
	local goos="${GOOS:-$(go env GOOS)}"
	local goarch="${GOARCH:-$(go env GOARCH)}"

	printf "%s/%s-%s-%s\n" "$dist_dir" "$binary_name" "$goos" "$goarch"
}

#!/usr/bin/env bash
# Shared Git metadata, linker flags, and target paths for build tasks.

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
	local version commit branch build_time build_by

	version="$(slack_version)" || return
	commit="$(slack_commit)" || return
	branch="$(slack_branch)" || return
	build_time="$(slack_build_time)" || return
	build_by="$(slack_build_by)" || return

	printf '%s' \
		"-s -w" \
		" -X '${module}/internal/version.Version=${version}'" \
		" -X '${module}/internal/version.Commit=${commit}'" \
		" -X '${module}/internal/version.Branch=${branch}'" \
		" -X '${module}/internal/version.BuildTime=${build_time}'" \
		" -X '${module}/internal/version.BuildBy=${build_by}'"
}

slack_binary_path() {
	local dist_dir="${DIST_DIR:?DIST_DIR is required}"
	local binary_name="${BINARY_NAME:?BINARY_NAME is required}"
	local goos="${GOOS:-}"
	local goarch="${GOARCH:-}"

	if [[ -z "$goos" ]]; then
		goos="$(go env GOOS)" || return
	fi
	if [[ -z "$goarch" ]]; then
		goarch="$(go env GOARCH)" || return
	fi

	printf "%s/%s-%s-%s\n" "$dist_dir" "$binary_name" "$goos" "$goarch"
}

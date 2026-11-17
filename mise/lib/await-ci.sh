#!/usr/bin/env bash
# Require successful main-push CI for the exact commit being released.
set -euo pipefail

sha="$(git rev-parse --verify "${TAG_NAME:?TAG_NAME is required}^{commit}")"
repository="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
runs_url="repos/${repository}/actions/workflows/ci.yml/runs?head_sha=${sha}&branch=main&event=push&per_page=1"
deadline=$((SECONDS + 900))
grace=$((SECONDS + 180))

echo "awaiting main-push CI for ${sha}"
while :; do
	if ! run="$(timeout 30s gh api "$runs_url" \
		--jq '.workflow_runs[0] // empty | "\(.status) \(.conclusion)"')"; then
		echo "cannot read CI status for ${sha}" >&2
		exit 1
	fi

	if [[ -z "$run" ]]; then
		if ((SECONDS >= grace)); then
			echo "no main-push CI run found for ${sha}; tag a commit that was a pushed main branch tip" >&2
			exit 1
		fi
	else
		status="${run%% *}"
		conclusion="${run#* }"
		case "$status" in
		completed)
			if [[ "$conclusion" == "success" ]]; then
				echo "main-push CI succeeded for ${sha}"
				break
			fi
			echo "main-push CI for ${sha} concluded: ${conclusion}" >&2
			exit 1
			;;
		queued | in_progress | requested | waiting | pending) ;;
		*)
			echo "unexpected CI status for ${sha}: ${status}" >&2
			exit 1
			;;
		esac
	fi

	if ((SECONDS >= deadline)); then
		echo "timed out waiting for main-push CI for ${sha}" >&2
		exit 1
	fi
	sleep 15
done

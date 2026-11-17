#!/usr/bin/env bash
# Exercise the release gate without contacting GitHub or waiting in real time.
set -euo pipefail

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
repository_root="$(cd -- "$script_dir/../.." && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf -- "$fixture_dir"' EXIT
mkdir -p "$fixture_dir/bin"

cat >"$fixture_dir/bin/git" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'rev-parse --verify v9.9.9^{commit}' ]] || exit 91
if [[ "$CI_GATE_SCENARIO" == git-error ]]; then
	exit 42
fi
printf '%040d\n' 1
SCRIPT

cat >"$fixture_dir/bin/gh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
expected_url='repos/example/slack-cli/actions/workflows/ci.yml/runs?head_sha=0000000000000000000000000000000000000001&branch=main&event=push&per_page=1'
[[ "$#" == 4 && "$1" == api && "$2" == "$expected_url" && "$3" == --jq ]] || exit 92
attempt=0
if [[ -f "$CI_GATE_FIXTURE/calls" ]]; then
	read -r attempt <"$CI_GATE_FIXTURE/calls"
fi
attempt=$((attempt + 1))
printf '%s\n' "$attempt" >"$CI_GATE_FIXTURE/calls"

case "$CI_GATE_SCENARIO" in
success) echo 'completed success' ;;
failure | cancelled | skipped | neutral) echo "completed $CI_GATE_SCENARIO" ;;
api-error) exit 43 ;;
api-timeout) exit 124 ;;
unexpected) echo 'unrecognized null' ;;
malformed) echo 'completed' ;;
missing) ;;
timeout) echo 'in_progress null' ;;
pending-then-success)
	if ((attempt == 1)); then
		echo 'queued null'
	else
		echo 'completed success'
	fi
	;;
missing-then-success)
	if ((attempt > 1)); then
		echo 'completed success'
	fi
	;;
*) exit 93 ;;
esac
SCRIPT

cat >"$fixture_dir/clock.sh" <<'SCRIPT'
#!/usr/bin/env bash
sleep() {
	if [[ "$CI_GATE_SCENARIO" == missing ]]; then
		SECONDS=181
	elif [[ "$CI_GATE_SCENARIO" == timeout ]]; then
		SECONDS=901
	fi
}
SCRIPT

cat >"$fixture_dir/bin/timeout" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == 30s ]] || exit 94
shift
exec "$@"
SCRIPT

chmod +x "$fixture_dir/bin/git" "$fixture_dir/bin/gh" "$fixture_dir/bin/timeout"
for script in "$fixture_dir/bin/git" "$fixture_dir/bin/gh" "$fixture_dir/bin/timeout" "$fixture_dir/clock.sh"; do
	bash -n "$script"
done
shellcheck "$fixture_dir/bin/git" "$fixture_dir/bin/gh" "$fixture_dir/bin/timeout" "$fixture_dir/clock.sh"

run_case() {
	local scenario="$1"
	local expected_status="$2"
	local expected_message="$3"
	local status=0
	local output

	rm -f -- "$fixture_dir/calls"
	output="$(
		PATH="$fixture_dir/bin:$PATH" \
			BASH_ENV="$fixture_dir/clock.sh" \
			CI_GATE_FIXTURE="$fixture_dir" \
			CI_GATE_SCENARIO="$scenario" \
			GITHUB_REPOSITORY=example/slack-cli \
			TAG_NAME=v9.9.9 \
			bash "$repository_root/mise/tasks/await-ci" 2>&1
	)" || status=$?

	if [[ "$status" != "$expected_status" || "$output" != *"$expected_message"* ]]; then
		printf 'FAIL %s: expected exit %s and message %s; got exit %s\n%s\n' \
			"$scenario" "$expected_status" "$expected_message" "$status" "$output" >&2
		exit 1
	fi
	if [[ "$scenario" == git-error && -f "$fixture_dir/calls" ]]; then
		echo 'FAIL git-error: GitHub was queried after tag resolution failed' >&2
		exit 1
	fi
	printf 'PASS %s\n' "$scenario"
}

run_case success 0 'main-push CI succeeded'
run_case pending-then-success 0 'main-push CI succeeded'
run_case missing-then-success 0 'main-push CI succeeded'
run_case failure 1 'concluded: failure'
run_case cancelled 1 'concluded: cancelled'
run_case skipped 1 'concluded: skipped'
run_case neutral 1 'concluded: neutral'
run_case api-error 1 'cannot read CI status'
run_case api-timeout 1 'cannot read CI status'
run_case unexpected 1 'unexpected CI status'
run_case malformed 1 'concluded: completed'
run_case missing 1 'no main-push CI run found'
run_case timeout 1 'timed out waiting for main-push CI'
run_case git-error 42 ''

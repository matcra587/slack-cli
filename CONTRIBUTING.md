# Contributing

## Setup

```bash
mise install
mise run deps
mise run build
```

`mise run build` writes `./dist/slick-<goos>-<goarch>`.
The repository is `slack-cli`; the installed command is `slick`, a short play
on `slack-cli`.

## Checks

```bash
mise run check
mise run test:all
mise run test:integration
mise run release:check
actionlint -color .github/workflows/*.yml
zizmor --persona pedantic --offline --no-progress --color never .github/
```

## Hooks

```bash
mise exec -- hk install --mise
mise run pre-commit
mise exec -- hk check --all --check
mise exec -- hk fix --all
```

## Common Tasks

```bash
mise tasks
mise run fmt
mise run lint
mise run lint:fix
mise run vet
mise run security
mise run deps:update
mise run release -- v0.8.1
```

## Commits

Use Conventional Commits:

```text
<type>[(scope)][!]: <subject>
```

Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `build`, `ci`,
`chore`.

## Pull Requests

Open pull requests against `main`. Describe what changed and why.

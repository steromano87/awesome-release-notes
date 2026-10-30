# Release notes

## 🌐 Global

### NOREF: [Global] Use new DEV runner group on all the shared workflows (#84)

The default runner group has been updated to the official DEV group with
the following specs:
```yaml
group: RunGrpGheHostedRunnersDev
label: hosted_runner_ubuntu_latest_dev
```

> [!WARNING]
> The old runner group will be dismissed by *December 1st 2026*. Ensure
to switch to the newer one in time.

---------

Co-authored-by: github-actions[bot] <github-actions[bot]@users.noreply.github.com>

## 🔧 Utilities

### MGA-2517: [Utilities] Add reusable workflow to trigger Jenkins multibranch reindexing when a push event happens (#79)

A new shared utility workflow is now available to trigger a Jenkins
Multibranch Pipeline reindex everytime an event occurs on GitHub.

This workflow allows teams to cover the gap with Azure DevOps, where
indexing was performed on demand every time a push was performed on a
branch.
Initially, teams had to manually poll GHE to discover changes, but this
created a lot of overhead on Jeknkins server. By using this workflow,
the on-demand branch discovery behaviour can be restored.

---------

Co-authored-by: github-actions[bot] <github-actions[bot]@users.noreply.github.com>
Co-authored-by: Stefano Romanò <s.romano2@crif.com>

## 🐛 Bugfixes"

### NOREF: [Bugfix] ✨ Nexus: fix upload, download and delete actions (#80)

The `nexus/upload`, `nexus/download` and `nexus/delete` actions fail on
every call, and `nexus/download` also corrupts binary artifacts. Found
while moving the CSR WebApp pipelines (packages on `D-Sharp_raw`) to the
shared workflows.

### Causes

- `upload` sends `POST`: Nexus raw repositories accept only `PUT` (`405
Method Not Allowed`).
- `upload`, `download` and `delete` read `steps.check.outputs.status`
(copied from `nexus/check`), but in these actions the HTTP step has
another id: `fromJSON('')` is a template error, so the action fails even
when the request succeeds (e.g. the file is deleted) and the telemetry
record is not written.
- `download`: `http-request-action` writes the response (`responseFile`)
as a string, which corrupts binary files.
- `check`: when the request fails (e.g. `401`) the `status` output is
empty and `fromJSON('')` hides the HTTP error behind a template error.

### Changes

Source templates only, the generated files are left to the internal CI.
Inputs, outputs and telemetry spec are unchanged.

- `upload`: `PUT`.
- `upload`, `download`, `delete`: `successful` from the outcome of the
HTTP step, no `fromJSON()` on outputs that can be empty.
- `check`: `is-present` compares the status as a string.
- `download`: `curl`, with the credentials through stdin and the
`timeout` input still in milliseconds; on failure the partial file is
removed.

### Validation

- `pnpm lint`, `pnpm build`, `pnpm test`, `pnpm package`: OK. zizmor
v1.30.1 with `.github/zizmor.yml` on the generated files: no findings.
shellcheck on the download script: clean.
- End to end on `hosted_runner_ubuntu_latest` against `D-Sharp_raw`,
`v1.0.1` compared with the actions generated from this branch (2 MB zip
of random bytes):

| Call | `v1.0.1` | This branch |
|---|---|---|
| upload | `405` plus template error, nothing stored | stored |
| download | template error, corrupted file (3,803,237 bytes instead of
2,097,644) | identical file (SHA-256) |
| delete | file deleted, but the action fails (template error) | success
|
| check, file present / missing | `true` / `false` | `true` / `false` |
| check, wrong password | template error | fails, `is-present=false` |
| download, missing file | not tested | fails, `successful=false`, no
leftover file |
| upload, wrong password | not tested | fails, `successful=false` |
| telemetry records written | 2 of 6 calls | 9 of 9 calls |

---------

Co-authored-by: github-actions[bot] <github-actions[bot]@users.noreply.github.com>
Co-authored-by: Stefano Romanò <s.romano2@crif.com>

### MGA-2559: [bugfix] End Of Life: fix output message and EOL check when no EOL date is set (#82)

Previously, when a language/SDK had no end of life date, a warning
appeared in run logs due to an incorrect parsing of the days until EOL.

Now a check has been added to catch when the EOL date is equal to None
and print informative message instead of a warning.

---------

Co-authored-by: Romanò Stefano <S.Romano2@crif.com>

# arn-gen (awesome-release-notes)

`arn-gen` turns Git commit messages into Markdown release notes. It reads a
local repository through `HEAD`, groups commits into categories, and writes
the result to standard output or a file.

You control which commits to include, how to categorize them, and how each
entry reads. Conventional Commits work with the included configuration, but
are not required. Each entry uses the original commit subject and body unless
you supply templates.

The CLI runs on Windows, Linux, and macOS, on amd64 and arm64. The standalone
binaries need neither a Git executable nor a Go runtime. Markdown is the only
output format; the CLI generates a document, not a published release.

- [Install and run](#install-and-run)
- [CLI options](#cli-options)
- [Commit selection](#commit-selection)
- [Configuration](#configuration)
- [Output and errors](#output-and-errors)
- [Development and releases](#development-and-releases)

## Install and run

Download and extract the archive for your platform from the project's GitHub
Releases. Put `arn-gen` (`arn-gen.exe` on Windows) on your `PATH`, then copy
the included [`.release-notes.yaml`](.release-notes.yaml) to the root of your
Git project.

Run it from that project:

```shell
arn-gen
arn-gen --title "Release 1.3.0" --from v1.2.0 --output RELEASE_NOTES.md
arn-gen --repo /path/to/project --config custom.yaml
```

The first command prints notes for commits since the nearest reachable tag.
The second chooses a starting tag and saves the document. The third uses
another repository and configuration file. Paths passed to `--repo`,
`--config`, and `--output` are relative to your current working directory.

In PowerShell, use paths such as `--repo C:\Projects\my-project`. If the binary
is in the current directory rather than on `PATH`, run `.\arn-gen.exe` on
Windows or `./arn-gen` on Linux/macOS.

With the included configuration, a commit message such as:

```text
feat(cli): add options

Support explicit tags and custom configuration paths.

- Add --from.
- Add --config.
```

appears under the CLI features subcategory. With a flat `Features` category
instead, the default item templates produce:

```markdown
## Features

### feat(cli): add options

Support explicit tags and custom configuration paths.

- Add --from.
- Add --config.
```

The full document starts with a level-1 release title. Commit bodies keep their
Markdown paragraphs, lists, and code blocks; commits without a body have only
an item heading.

## CLI options

| Flag | Default | Purpose |
| --- | --- | --- |
| `--repo` | `.` | Repository or any directory inside it. |
| `--config` | Repository-root `.release-notes.<extension>` | JSON, YAML, or TOML configuration path. |
| `--from` | Nearest reachable tag | Starting tag, excluded from the range. |
| `--format` | `markdown` | Output format; only `markdown` is supported. |
| `--output` | Standard output | File to create or overwrite; parent directory must exist. |
| `--title` | `Release notes` | Non-empty, single-line release title. |
| `--version` | | Print the binary version. |
| `--help` | | Print usage. |

Set the release title with `--title`, not in the configuration file.

## Commit selection

The range always ends at `HEAD`, including a detached HEAD. It includes commits
from merged branches and merge commits, but not unmerged branches or uncommitted
changes. The repository must have at least one commit and a working tree;
bare repositories are not supported.

`--from` accepts an exact tag name, not a branch, hash, or revision expression.
The tag must point to a commit reachable from `HEAD`. That commit and its
ancestors are excluded from the notes.

Without `--from`, the CLI chooses the reachable tag that leaves the fewest
commits in the range. This is a commit-graph comparison, not a search for the
highest version number or the newest tag. Ties prefer annotated tags, then the
most recent tagger date, then the lexicographically smallest tag name.
Lightweight and annotated tags are supported, including nested annotated tags.
Tags pointing to trees or blobs are ignored during automatic selection and
rejected when passed to `--from`.

If no tag is reachable, the notes include all commits reachable from `HEAD`.
If `HEAD` is at the selected tag, the range is empty. An empty range produces
the title and any frontmatter/backmatter, without category headings.

Commits follow the reverse of Git's committer-time traversal, oldest first,
and keep that order within each category.

### CI checkouts

Fetch complete history and tags before running the CLI. For
`actions/checkout`, set `fetch-depth: 0`. Shallow or incomplete commit history
causes an error rather than incomplete notes. Missing tags can change the
automatically selected range, so fetching commits alone is not enough.

## Configuration

A configuration file is required. By default, the CLI looks in the Git
repository root, even when you run it from a subdirectory. It checks these
names in order and uses the first file it finds:

1. `.release-notes.json`
2. `.release-notes.toml`
3. `.release-notes.yaml`
4. `.release-notes.yml`

Files are not merged. An invalid selected file causes an error; the CLI does
not try the next format. Use `--config custom.toml` to select a file explicitly.
An explicit path can have any basename, but must use one of the extensions
above. Process environment variables do not override file settings.

Start with the included [`.release-notes.yaml`](.release-notes.yaml), which
accepts Conventional Commits and ordinary commit messages. This shorter
example requires Conventional Commit subjects:

```yaml
frontmatter: |
  Highlights for this release.
backmatter: |
  Thanks to everyone who contributed!
commitPattern: '^(?P<type>\w+)(?:\((?P<scope>[^)]+)\))?: (?P<description>[^\n]+)'
ignoreUnmatchedCommits: false
excludedPatterns: []
itemTitleTemplate: '{{ .Subject }}'
itemBodyTemplate: '{{ .Body }}'
sectionHeaderLevel: 2
itemHeaderLevel: 3
uncategorizedTitle: Other changes
categoryCaseInsensitive: false
categories:
  - name: Features
    patterns: ['^feat(?:\([^)]+\))?:']
  - name: Fixes
    patterns: ['^fix(?:\([^)]+\))?:', '^revert:']
```

### Settings reference

Keys are case-insensitive; the documented spelling is camelCase. Omit optional
keys to use their defaults. Unknown keys, wrong value types, null values, invalid
regexes, and invalid templates are errors. YAML must contain a single mapping
document: a blank value such as `itemTitleTemplate:` is null, not a default.

| Key | Default | Meaning |
| --- | --- | --- |
| `frontmatter` | Empty | Literal Markdown between the release title and categories. |
| `backmatter` | Empty | Literal Markdown after all categories. |
| `commitPattern` | `^(?P<description>[^\n]+)` | Go regex for extracting fields from each commit message. |
| `ignoreUnmatchedCommits` | `false` | Skip commits that do not match `commitPattern` instead of failing. |
| `excludedPatterns` | Empty list | Regexes for excluding commits before parsing. Any match excludes the commit. |
| `itemTitleTemplate` | `{{ .Subject }}` | Go text template for each item title, without the Markdown heading marker. |
| `itemBodyTemplate` | `{{ .Body }}` | Go text template for the content below each item heading. |
| `sectionHeaderLevel` | `2` | Base heading level for top-level categories and the uncategorized section. |
| `itemHeaderLevel` | `3` | Base heading level for items in top-level categories. |
| `categories` | Empty list | Ordered category tree. Leaves require `patterns`; parents can contain nested `categories`. |
| `categoryCaseInsensitive` | `false` | Ignore case in category patterns at every depth. Does not affect parsing or exclusions. |
| `uncategorizedTitle` | `Other changes` | Heading for commits not matched by a category. Must differ from every category name. |

Heading levels must be integers from 1 to 6. YAML and TOML require integer
literals; JSON also accepts integer-valued numbers such as `3.0`.

For editor validation, use the
[JSON Schema](internal/config/release-notes.schema.json), included in release
archives as `release-notes.schema.json`. If it is next to your YAML file,
editors supporting YAML Language Server can use:

```yaml
# yaml-language-server: $schema=release-notes.schema.json
```

The CLI embeds the schema and validates configuration locally. You do not need
to install the schema separately or access the network to run the CLI.

### Parsing and exclusions

All regexes run against the full commit message: the subject, followed by
`\n\n` and the body when present. They use
[Go's regexp syntax](https://pkg.go.dev/regexp/syntax), without lookaround or
backreferences. Use `(?m)` for line anchors inside a body and `(?s)` to let `.`
match newlines. Single-quoted YAML strings preserve backslashes.

To omit merge commits, release-maintenance commits, and commits with a
`skip-release-notes` marker in the body:

```yaml
excludedPatterns:
  - '^Merge '
  - '^chore\(release\):'
  - '(?m)^skip-release-notes$'
```

A match with any exclusion pattern removes the commit before parsing,
categorization, or template rendering. An omitted or empty list excludes
nothing. Empty regexes are invalid; use `'.*'` to exclude everything.

`commitPattern` uses the first regex match. Named captures become
`.Fields.<name>` in templates; unmatched optional captures are empty strings.
Capture names must be unique.

A commit that does not match `commitPattern` stops generation by default.
Set `ignoreUnmatchedCommits: true` to skip it. This setting does not suppress
validation or template errors for matching commits. If every commit is
excluded or skipped, the document contains only the title and any
frontmatter/backmatter.

### Categories

Categories are tried in configuration order. The first match wins, so each
commit appears once. Patterns within a category act as OR conditions.
Commits that match no category appear last under `uncategorizedTitle`.
For an explicit catch-all category, put `'.*'` in the last category's patterns.

Patterns are case-sensitive unless `categoryCaseInsensitive: true`. That
setting applies at every depth, including parent fallback patterns. It uses
Go's `(?i)` flag; explicit inline flags can override it.

Categories can contain subcategories:

```yaml
categories:
  - name: Changes
    categories:
      - name: Features
        patterns: ['^feat']
        categories:
          - name: API features
            patterns: ['^feat\(api\):']
          - name: CLI features
            patterns: ['^feat\(cli\):']
      - name: Fixes
        patterns: ['^fix:']
```

Children are tried before their parent's patterns. They do not need to match a
parent pattern. A parent's own patterns act as a fallback for direct items;
an earlier sibling's subtree or fallback takes precedence over later siblings.
`Changes` above has no patterns and serves only as a grouping heading.
Every leaf needs at least one pattern.

Names must be non-empty single lines and unique across the whole tree,
including `uncategorizedTitle`. Empty categories are omitted, while ancestors
of populated categories remain. Direct items render before subcategories;
sibling categories keep configuration order.

At depth `d`, starting at 0, category headings use `sectionHeaderLevel + d`
and item headings use `itemHeaderLevel + d`. With the defaults, the API feature
in the example renders as:

```markdown
## Changes

### Features

#### API features

##### feat(api): add endpoint
```

Choose a deeper item level than the section level for a nested hierarchy.
Configurations whose category or item levels would exceed 6 are rejected.
The release title stays at level 1; headings inside commit bodies are untouched.

### Item templates

`itemTitleTemplate` and `itemBodyTemplate` use Go's standard
[`text/template`](https://pkg.go.dev/text/template). Both have access to:

| Field | Value |
| --- | --- |
| `.Hash`, `.ShortHash` | Full hash and a prefix of at least seven characters, unique among commits reachable from `HEAD`. |
| `.Subject`, `.Body` | Commit subject and body, without trailing line endings. |
| `.Message` | Subject plus `\n\n` and body, when present. |
| `.Author` | Author name. |
| `.Date` | Author date as `time.Time`, e.g. `{{ .Date.Format "2006-01-02" }}`. |
| `.Fields` | Named regex captures, e.g. `{{ .Fields.description }}`. |

The default title keeps the complete subject, including any Conventional
Commit prefix, scope, or `!`. The default body is the text after the subject
and blank line, not Git Notes or GitHub comments.

You can change either template independently. For example,
`itemTitleTemplate: '{{ .Fields.description }} ({{ .ShortHash }})'` uses the
captured description and short hash while leaving the body unchanged.
Keep capture references in sync with `commitPattern`.

Templates render plain text without Markdown escaping. Accessing a missing
field with dot notation or an execution error stops generation. Empty or
whitespace-only template strings are invalid; omit the key to use its default.
An item title must render a non-empty single line. Outer line endings from
YAML block templates are removed. A body that renders empty or whitespace-only
leaves just the item heading.

## Output and errors

`frontmatter` and `backmatter` are literal Markdown blocks, not templates or
YAML metadata. The CLI separates document blocks with a blank line and ends
the document with a newline. Output uses LF on every platform, including text
from configuration and templates.

The CLI renders the whole document before writing it. Parsing or rendering
errors therefore do not emit partial notes or overwrite an existing output
file. File output uses a temporary file in the destination directory, then
renames it over the destination after writing and flushing. A failed write
does not truncate existing notes. Existing file permissions are retained;
symbolic links are followed to their targets, and dangling links are errors.

Notes go to standard output unless you pass `--output`. Logs and diagnostics
go to standard error.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Generation, Git, configuration, or I/O error. |
| `2` | Invalid command-line arguments or unsupported output format. |

## Development and releases

Development requires Go 1.26+. The CLI is written in Go with Cobra, Viper,
and go-git. Tests create repositories with go-git and do not need a Git
executable.

```shell
go build -o arn-gen .
go test ./...
go vet ./...
goreleaser check
goreleaser release --snapshot --clean
```

On Windows, build with `go build -o arn-gen.exe .`. Local builds report `dev`
for `--version`; release builds report the packaged version.

Release packaging requires Git and GoReleaser v2, plus a checkout with at least
one commit and an `origin` remote identifying the GitHub repository. Publishing
also requires a release tag and a `GITHUB_TOKEN` with permission to create
releases. Snapshot builds do not publish anything.

[`.goreleaser.yaml`](.goreleaser.yaml) builds with `CGO_ENABLED=0` for the six
OS/architecture combinations listed above. Windows archives use ZIP;
Linux/macOS archives use tar.gz. Each includes the binary, this README,
the sample configuration, and its schema. Releases also include SHA-256
checksums.

CI checks formatting, runs `go vet` and tests with the race detector on Linux,
Windows, and macOS, and checks GoReleaser configuration on Linux. Pushing a
`v*` tag creates a draft GitHub Release; review and publish it manually.

# Working in this repository

This is the starter kit. It is a `package main` whose whole job is to write
files into somebody else's project:

```sh
go run github.com/arandu-io/ui@latest auth
```

Nothing here is imported at run time, and nothing here is a library. Every file
this command writes belongs to the project it lands in from the moment it lands
— it is edited there, and this repository never sees the edit. That is the
difference from working on an application or on a component library, and it is
what the rules below are for.

The consequence is the one thing to hold on to: **the templates in this
repository are somebody else's source code.** A shortcut taken here is a
shortcut in a file a stranger will open on their first day and own for years.

Read `.agents/skills/` before writing code. Each skill is a procedure, and the
one you need is named by the situation you are in.

## The gates

Nothing is finished until all six exit zero.

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
bash tests/test-layout-guard.sh
go test . -update && git status --porcelain testdata   # has to print nothing
```

The last one is not a formality. The golden files under `testdata/auth/` are the
bytes that land in somebody's project, so a change to a template has to appear
as a diff in review rather than as a surprise in a checkout. CI runs `-update`
and fails if the tree moved.

`go test` is where the published Go is compiled.
`TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework` lays the
nine Go files into a throwaway module that requires the framework and the
component library by **published tag, with no `replace`**, and runs `go build`
against it. Read its doc comment before touching it: a `replace` pointed at
`../framework` compiles the working tree, which is the one framework nobody
receives, and a `.kyse.go` written into that module would pass green because the
compiler never reads one. If it prints `--- SKIP`, the tags are not in the module
cache and there is no network — nothing was compiled, and that is not a pass. CI
runs that one test again on its own and fails on the skip, because a release that
was never compiled is the defect it exists to prevent.

`aru view:build` and `aru doctor` are **not** gates here, and asking for them is
a wasted minute:

- `aru view:build` exits 0 and prints nothing. There is no `.kyse.go` file in
  this tree — every view is a Go string constant, and the ones under `testdata/`
  end in `.golden`.
- `aru doctor` exits 1 with *this is not an Arandu project*. It reads an
  application, and this is a publisher.

Both `gofmt` filters are carried from the rest of the project rather than earned
here: with zero `.kyse.go` files and zero `.go` files under `testdata/`,
`gofmt -l .` exits 0 too. Keep them anyway — the day a real view or a real Go
fixture arrives, the command is already right.

## What this repository holds

| | measured with |
| --- | --- |
| 5 Go source files, one package, no subdirectories | `ls *.go \| grep -v _test.go \| wc -l` |
| 22 template constants, one per file the command writes | `grep -ho 'const [a-zA-Z]*Template' views*.go \| wc -l` |
| 22 files published by `auth` — 13 views and 9 plain Go | `go build -o /tmp/ui . && (cd ../arandu && /tmp/ui auth --dry-run \| wc -l)` |
| 16 of those refreshed by `auth --views` | `(cd ../arandu && /tmp/ui auth --views --dry-run \| wc -l)` |
| 22 golden files, byte for byte what is published | `find testdata -name '*.golden' \| wc -l` |
| 61 tests in 4 internal test files | `grep -h '^func Test' *_test.go \| wc -l` |
| 14 routes mounted by the module it publishes | `grep -hE '^\tg\.(Get\|Post)\(' views_controllers.go views_auth_flow.go \| wc -l` |
| 0 dependencies, and that is a CI step | `grep -E '^[[:space:]]*require' go.mod \| wc -l` |
| 5 files replaced without `--force`, the layout unit | `sed -n '/^var replaced/,/^}/p' publish.go \| grep -c 'true,'` |

Of the 13 views, 9 are screens — the layout, home, welcome, and the six auth
screens — and 4 are message bodies, an HTML part and a plain-text part for each
of the two messages the flow sends. A mail body is not a screen: it has no
layout, no navigation and no token, and `TestTheAuthViewsAreNineAndWellFormed`
in `views_internal_test.go:20` counts the two groups separately for that reason.

## What does not exist here

Reaching for one of these is the most common way to waste an afternoon. None of
them is missing by accident; each was considered and refused.

| A model reaches for | What is here instead |
| --- | --- |
| a `resources/views/` directory to edit | a Go string constant. `authLoginViewTemplate` in `views.go` **is** `resources/views/auth/login.kyse.go` |
| a dependency — a template library, a CLI framework, a diff library | nothing. `go.mod` has no `require` block, a CI step fails if one appears, and every require here is a download for everyone who runs the command |
| importing the CLI's renderer | a 40-line copy in `publish.go`. Importing it would make this module depend on the CLI, and the point of publishing from here is that the CLI is not in the way |
| a preset argument — `auth bootstrap`, `auth tailwind` | one set of screens. `auth` with an extra word is refused rather than ignored, because a flag typed after an ignored word is switched off silently |
| a generator that edits `bootstrap/app.go` | text printed to the terminal for a person to paste. One line somebody reads beats a file edited behind their back |
| `os.WriteFile` over an existing file | `write` in `publish.go`, which reads the file first and carries the custom blocks over. It used to stat and overwrite, and it ate people's work |
| a `_test.go` beside the code with no `_internal` suffix | `tests/test-layout-guard.sh`, which fails the build. This is a `package main`, so its tests are internal and the suffix says so |

## The two rules everything else follows from

**Every published file is the project's, and a republish must prove it.** The
command is meant to be run again for a fix from a newer version. That only works
because what somebody wrote inside `arandu:begin custom` … `arandu:end custom`
is carried forward, and because five files — the layout unit — are the only ones
replaced without a flag. Nine of the 22 published files carry such a block: five
in Go comment syntax, four in kyse comment syntax, because a `//` below the
package clause of a `.kyse.go` is markup that would be printed into an e-mail.

**The golden files are the product.** They are not a convenience for the suite;
they are the 22 files a project receives. `TestAuthGolden` in
`publish_internal_test.go:28` compares them byte for byte, and CI regenerates
them and fails on a dirty tree.

## Writing code

Comments, identifiers, error messages, CLI output, the text of a published
screen and test names are in English. So is everything a template emits — it
becomes source in somebody's repository.

Nothing published may carry the framework's name. The verification mail once
shipped the literal word, so every project running this command signed its first
message to its own users with the name of the framework. The brand is a field,
filled from the application's configuration, and
`TestNothingTheKitPublishesIsBrandedWithItsOwnName` in
`flow_internal_test.go:346` reads every published file to keep it that way.

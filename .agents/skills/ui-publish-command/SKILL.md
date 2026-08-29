---
name: ui-publish-command
description: The command that writes the Arandu starter kit into a project, and what a republish refreshes versus leaves alone. Use when the request is to "add a file to the kit", "publish another view", "change what --force does", "why was my file not overwritten", "my edit was lost", "the custom block did not survive", "add a flag", "change the wiring instructions", or when a pull request touches publish.go, main.go or GenerateAuth. Covers the 28 files it writes, the 5 replaced with no flag, the 21 that --views refreshes, how merge carries a custom block over in two comment syntaxes, and why nothing is added to the caller's go.mod.
license: MIT
---

# The publish command

```sh
go run github.com/arandu-io/ui@latest auth
```

Nothing is added to the caller's `go.mod`: `go run <module>@latest` runs a
published module without touching the dependency graph, so there is no package
to install and none to remove. That property is the reason this is a package
rather than a subcommand of the CLI — in Go what you import you cannot edit, and
editing the sign-in screen is the first thing anyone does.

The command is meant to be run **again**, for a fix from a newer version.
Everything below exists to make the second run safe, because nobody runs a
command twice after it has eaten their work once.

## What it writes

Measured from the skeleton beside this checkout:

```sh
export GOWORK=off
go build -o /tmp/ui . && (cd ../arandu && /tmp/ui auth --dry-run)
```

28 files: 18 views and 10 plain Go. The Go set is five controller files in
`app/Http/Controllers/Auth/` (`LoginController.go`,
`LoginController_handlers.go`, `RegisterController.go`,
`PasswordController.go`, `TwoFactorController.go`), `render.go` and `page.go`
beside them, the two mailables in `app/Mail/`, and
`app/Http/Controllers/HomeController.go`.

`GenerateAuth` in `views_controllers.go:18` is the list of the ten Go files;
`AuthViews` in `views.go:110` is the list of the eighteen views plus `page.go`.
There are exactly 28 template constants, one per published file
(`grep -ho 'const [a-zA-Z]*Template' views*.go | wc -l`), and 28 golden files
under `testdata/auth/`.

`--dry-run` prints the list and writes nothing. Use it before anything else.

## The three states a file can be in

`write` in `publish.go:379` reads the file on disk — it does not stat it — and
that read is what decides:

| state | when | reported as |
| --- | --- | --- |
| **written** | nothing there, or `--force`, or the path is in `replaced` | `wrote <path>` |
| **kept** | it exists, no `--force`, not in `replaced` | `kept <path> (exists; --force overwrites)` |
| **merged** | it was written over and the old file had a custom block whose content differed | `wrote <path> (your custom block was carried over)` |

Publishing twice into the same project, with no flag, writes 5 and keeps 23.
That is not a convenience. In kyse a page renders **with the type of its
layout**, so the layout and everything that extends it are one unit; publishing
a new layout beside the old pages leaves a project that builds and fails to
render. `replaced` in `publish.go:288` spells the five out rather than inferring
them, so a sixth cannot join quietly:

```
resources/views/layouts/app.kyse.go
resources/views/home.kyse.go
resources/views/welcome.kyse.go
app/Http/Controllers/HomeController.go
app/Http/Controllers/Auth/page.go
```

## `--views`: the screens, not the flow

21 of the 28. The 18 views, plus the three Go files the layout unit does not
compile without: `page.go`, `render.go` and `HomeController.go`. What it leaves
alone is the flow — the five authentication controller files and the two
mailables, which are the files somebody edits to decide their own rules.

`render.go` was once left out, which made `--views` the flag that writes code
that does not compile: the `HomeController` it publishes calls `authui.Chrome`
and `authui.SignedInName`, and both are declared there. A project that reached
for the safe flag before it had ever run the full command got a build failure
naming two symbols it had never heard of. It is safe to include precisely
because it is **not** in `replaced`: an existing one is kept and reported as
kept, so adding it changed what a project *without* one gets and nothing else.

`screensOnly` in `views.go:1319` is the filter, and
`TestViewsOnlyPublishesTheScreensAndTheLayoutUnit` at
`publish_write_internal_test.go:239` holds it to the list.

## The custom block

The escape hatch: what does not fit the standard shape is written inside
markers, and a republish carries it forward. 9 of the 28 published files have
one — 5 in Go comment syntax, 4 in kyse comment syntax:

```go
// arandu:begin custom
// arandu:end custom
```

```
{{-- arandu:begin custom --}}
{{-- arandu:end custom --}}
```

The syntax is the file's own, because a comment is not portable between the
two: `//` below the package clause of a `.kyse.go` is markup, and would be
printed to the reader of an e-mail. `markerFor` at `publish.go:324` picks by
extension, and the two regular expressions are never both run against one file —
a single alternation would happily pair a Go begin with a kyse end, because RE2
has no backreference to stop it, and the region between them is whatever
happened to be in the middle.

`merge` at `publish.go:337` matches blocks **by position, not by name**. That is
the honest limitation: reordering a generated file would shuffle them. Each
template puts one block per file, so the ordering has nothing to get wrong —
keep it that way.

**Adding a block to a template changes the contract of that file.** Before you
do, read `TestEveryPublishedMarkerIsOneMergeCanRead` at
`publish_write_internal_test.go:187`, and check that the block is where somebody
would actually edit. The four mail bodies were the reason `markerFor` exists:
they carry their block in kyse comments, the merge knew only the Go form, and
theirs were the blocks most likely to be edited — the wording of the message a
project sends.

## What it prints, and why it prints rather than writes

After the files, the command prints the wiring: an import line, the
`authui.New(...)` call to put in `k.Register(...)`, the
`controllers.NewHomeController(...)` call, and one blank import per directory of
views. One line in a file the person reads beats a generator that edits
`bootstrap/app.go` behind their back, whose output nobody can then explain.

Two things about that text are checked by the suite rather than by review,
because both have shipped wrong:

- `wiring` is a `const` in `main.go:219` rather than a literal inside the
  `Printf`, so a test can read it.
  `TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes` at
  `publish_internal_test.go:522` parses both the printed call and the published
  constructor and compares the arity. It shipped with three parameters emitted
  and five passed.
- The blank-import block is computed by `viewImports` in `main.go:68` from what
  `AuthViews` actually writes, never typed out. A view added to the kit cannot
  ship with an instruction that does not mention it — which is how a project
  ends up answering 500 with *no view named auth.verify* on a screen the kit
  just installed.

## Adding a file to the kit

1. Write the template constant and add the entry to `GenerateAuth` or
   `AuthViews`.
2. Decide whether it belongs in `replaced` — only if the layout unit does not
   render without it — and whether `screensOnly` keeps it.
3. `go test . -update`, then read `git diff testdata`. There should be exactly
   one new golden file per new published file.
4. If it mounts a route, the route and its name go in the table at
   `flow_internal_test.go:946`, which is exact and ordered on purpose.
5. Run the gates.

## The arguments it accepts, and the one it refuses

`auth` takes `--force`, `--views` and `--dry-run`. Anything left over after flag
parsing is an error, not a shrug: `flag.Parse` stops at the first non-flag
argument and returns the rest without complaining, so `auth bootstrap --force`
would publish with force **off** and say nothing — the word ignored, and every
flag typed after it switched off with it. Answering nothing is worse than
refusing, because the person believes the flag took.

There is no preset argument, and adding one is the wrong instinct: there is one
stack, and an argument with a single legal value is a dimension that does not
exist. The verb is what varies.

## Where it runs

`projectRoot` at `publish.go:90` walks up from the working directory looking for
`go.mod`, `main.go` and `arandu.toml` **together** — any one alone is a Go
module, a program, or a directory somebody copied a config into. Run from a
subdirectory it still writes into the project root, not the current directory.
Run outside a project it exits 1 with *this is not an Arandu project*.

## This module takes no dependencies

Not a style preference: it is run from inside somebody's project, so every
`require` here is something they download to publish 28 files. A CI step fails
on one. That is why `render` in `publish.go:44` is a copy of the CLI's renderer
rather than an import — importing it would put the CLI back in the way — and why
the golden files exist, comparing the published output byte for byte so that
what drifts is caught where it matters.

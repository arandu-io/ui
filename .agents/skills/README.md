# Skills

Procedures an assistant follows when working on this starter kit.

They live in `.agents/skills/<name>/SKILL.md`, which is the path the coding
assistants read from — Cursor, Codex, Cline, Copilot, Gemini CLI, Amp, OpenCode,
Warp, Zed and the rest all look there. It is one directory rather than a file
per vendor, so a skill written once is read by whatever this project is being
written with.

Each file opens with frontmatter carrying a `name` and a `description`. The
description is what a tool reads to decide whether the skill is relevant, so it
names the situation you are in rather than the topic it covers.

| skill | when it fires |
| --- | --- |
| `ui-published-view` | changing a screen or a message body: the markup, a field, the styling |
| `ui-publish-command` | the command itself: which files are written, refreshed, kept or merged |
| `ui-published-flow` | changing a handler, a route, a constructor or the wiring the command prints |

## Why these exist

The audience of this repository is somebody changing what other people receive,
not somebody receiving it. That inverts the usual question. There is no
`resources/views/auth/login.kyse.go` to open here — there is
`authLoginViewTemplate`, a Go string constant in `views.go`, and the file it
becomes is written into a stranger's repository, where they edit it, keep it,
and never send the edit back.

A model asked to change a screen here reaches for the file that does not exist,
edits the golden output instead of the template that produces it, or fixes a
screen while leaving the handler that fills it — and the failure is not a build
error in this repository. It is a form with `action=""` in somebody's project,
answering 200 to a browser and doing nothing.

The rest of the answer is that the kit is built to be checked rather than
trusted. Every published file is compared byte for byte against a golden file;
every URL field a screen reads is checked to be filled in by a handler; every
route the module mounts is compared against a table somebody wrote by hand; and
the constructor the command prints instructions for is compared against the
constructor it actually emits, and against the two projects checked out beside
this one. An assistant that runs `go test -race ./...` is not guessing.

## Adding your own

A skill in this directory is yours and travels with the repository. Keep it a
procedure rather than a description: a file that says "read the documentation"
never changes what anybody does.

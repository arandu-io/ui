# Contributing

## Sign your commits

Every commit needs a `Signed-off-by` line:

```
git commit -s -m "what changed and why"
```

That line is the [Developer Certificate of Origin](https://developercertificate.org/):
you are stating that you wrote the change, or that you have the right to submit
it under this project's license. It is not a copyright assignment — you keep
your copyright, and this project can never be relicensed behind your back.

We use DCO rather than a CLA on purpose. A CLA would let the project relicense
later, and the price is that every contributor has to sign a legal document
before their first patch.

## Before you open a pull request

```
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go vet ./...
go test -race ./...
```

CI runs these, and a handful of checks besides that are cheaper there than on
your machine; `.github/workflows/ci.yml` is the list, and it is the one that
decides. One of them is worth knowing before you write the change: this module
takes no dependencies at all. It is run as `go run github.com/arandu-io/ui@latest
auth` from inside somebody's project, so every require here is something a user
downloads. A pull request that adds one needs to argue for it first, in an issue.

## Where a test goes

Under `tests/`, or beside the code named `*_internal_test.go` -- and which of the
two follows from what the test has to reach, not from taste.
`tests/test-layout-guard.sh` runs in CI and rejects the third case: a
`*_test.go` outside `tests/` that does not carry the `_internal` suffix.

That suffix is a claim, and it is what earns the file its place. A test that
needs an identifier the package does not export has to compile into that
package, and a package compiles from a single directory -- so such a test has
nowhere else it could sit. `go test` attributes coverage per directory, which
points the same way: filed anywhere else it would leave the package under test
reporting 0%. The name states that reason in the one place a reader walking the
tree is certain to look.

Which package the test declares and where the file lands are one decision, not
two:

| declare | where it lands | when |
|---|---|---|
| `package X_test` | `tests/` | this is the **contract**. The test sees what a caller sees, which is the point |
| `package X` | beside the code, `*_internal_test.go` | this is the **implementation**, and the test genuinely needs something the package does not export |

Prefer the first. Take the second only when you use it -- `plans/testpackages.go`
in the arandu-io working tree checks exactly that, by intersecting the
identifiers a test names with what its package declares unexported, and the
checklist runs it across every Go repository in the project.

A `package main` has no external form: it cannot be imported, so its tests are
internal and that is the end of it. This module is one, so the first row above
describes nothing here and `tests/` carries the guard rather than a suite -- the
alternative is a category directory standing empty, which git does not carry and
for which a placeholder file only invents surface.

## What the commit message says

What changed and why. The why is the part that is not in the diff, and it is the
part someone will need in two years.

No AI attribution of any kind: no `Co-Authored-By` for an assistant, no
"generated with" footer. Commits are authored by the people who submit them.

## Architecture decisions

The decisions this project has already made live in `arandu-io/docs`, and every
one that closed a door has an ADR. If your change contradicts one, say so in the
pull request and argue for the change of decision — that is a normal thing to
do, and it is better than a patch that quietly works around it.

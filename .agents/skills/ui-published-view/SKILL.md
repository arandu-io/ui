---
name: ui-published-view
description: Change a screen or a message body that the Arandu starter kit publishes into somebody's project. Use when the request is to "change the login screen", "restyle the sign-in page", "add a field to the register form", "fix the layout", "edit the verification email", "change the password reset markup", "the welcome page", "add a link to the nav", or when a pull request touches views.go, views_auth_flow.go or resources/views in a project that ran this kit. There is no resources/views directory here — every view is a Go string constant rendered with <% %> delimiters. Covers where each screen lives, the AuthPage struct a screen may read, the directives and helpers kyse does not have, and updating the golden files.
license: MIT
---

# Changing a published screen

There is no file to open. `resources/views/auth/login.kyse.go` does not exist in
this repository — it is produced by `authLoginViewTemplate`, a Go string constant
in `views.go`, and written into a stranger's project, where they own it.

That is the whole reason to be careful here. A screen you ship is a file
somebody reads on their first day and keeps for years. Whatever shape you write
is the shape their next form copies.

## Where each screen lives

`AuthViews` in `views.go:55` is the list, and it is the map from constant to
published path. Thirteen views come out of it, plus `page.go`, the struct they
all render from:

| published path | constant |
| --- | --- |
| `resources/views/layouts/app.kyse.go` | `authLayoutViewTemplate` |
| `resources/views/home.kyse.go` | `authHomeViewTemplate` |
| `resources/views/welcome.kyse.go` | `authWelcomeViewTemplate` |
| `resources/views/auth/login.kyse.go` | `authLoginViewTemplate` |
| `resources/views/auth/register.kyse.go` | `authRegisterViewTemplate` |
| `resources/views/auth/verify.kyse.go` | `authVerifyViewTemplate` |
| `resources/views/auth/passwords/confirm.kyse.go` | `authPasswordConfirmViewTemplate` |
| `resources/views/auth/passwords/email.kyse.go` | `authPasswordEmailViewTemplate` |
| `resources/views/auth/passwords/reset.kyse.go` | `authPasswordResetViewTemplate` |
| `app/Http/Controllers/Auth/page.go` | `authPageTemplate` |

The four message bodies are in `views_auth_flow.go`: `verifyMailViewTemplate`,
`verifyMailTextTemplate`, `passwordMailViewTemplate`, `passwordMailTextTemplate`.
They are not screens — no layout, no navigation, no token — and the suite counts
them separately.

## The two levels of template, and this is the one trap

A view constant is rendered twice. First by this program, then by kyse in
somebody's project:

- **`<% %>` is this generator.** `render` in `publish.go:44` switches the
  delimiters for any name ending `.kyse.go`, so the only thing it interpolates
  is `<% .ModulePath %>` in the import block.
- **`{{ }}` is kyse**, in the project, and it survives into the published file
  untouched. That swap is why it can: without it, `{{ .Email }}` in a view would
  be read as an action of this generator and generation would fail on markup
  that is correct.

So `{{ .Email }}` in a constant here is markup. `<% .Email %>` is a bug.

## The procedure

**1. Edit the constant.** Keep the `//go:build kyse` header and the package
clause — the package is the directory's, because the generated Go sits beside
the source and one directory is one Go package. `auth/login.kyse.go` is
`package auth`; a file under `resources/views/` itself is `package views`.

**2. Read what the screen is allowed to read.** `AuthPage` in `views.go:135` is
the struct, published to `app/Http/Controllers/Auth/page.go`. It embeds
`view.Page` for the chrome — title, description, token, navigation — and adds
the form state: `Status`, `Name`, `Email`, `Remember`, `ResetToken`, the six
`…URL` fields, and one `…Error` field per input.

If a screen needs something new, add the field to `authPageTemplate` **and fill
it in from a handler in the same change.** A URL field read by a template and
assigned by nobody renders `action=""`, which posts to the current URL and looks
like it worked. `TestEveryAddressAScreenReadsIsFilledInSomewhere` in
`flow_internal_test.go:272` fails on either half — read and never filled, filled
and never read — and `TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn` at
`flow_internal_test.go:452` does the same for the message fields.

**3. Run the gates, then update the goldens.**

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go') \
  && go build ./... && go vet ./... && go test -race ./... \
  && bash tests/test-layout-guard.sh
go test . -update
git diff testdata     # this is the diff a reviewer reads
```

The golden files under `testdata/auth/` are the bytes that land in somebody's
project. Never edit one by hand: it is generated output, and CI regenerates
every one and fails if the tree moves.

**4. See it render, if the change is more than wording.** Nothing in this
repository compiles kyse — the compiler is internal to the CLI, and this module
takes no dependencies. `plans/prova-ponta-a-ponta.sh` in the working tree
publishes the kit into a generated project, runs the real `aru view:build`, the
real `go build`, and renders every page over HTTP. That is the only place a
markup error surfaces before somebody else's build.

## What kyse does not have

`TestTheAuthViewsInventNoDirective` at `views_internal_test.go:148` reads every
view and fails on any of these:

`@vite` `@auth` `@guest` `@error` `@can` `@props` `@stack` `@push` `@forelse`
`@switch` `@fonts` `<x-`

`TestTheAuthViewsReachForNoHelper` at `views_internal_test.go:183` fails on
`config(` `route(` `auth()` `__(` `old(` `session(` `Route::has`. Everything a
screen shows came from the handler, in the struct. The guest branch of the
navigation is `@if(!.SignedIn())`; a validation message is asked for by the
component, through `FieldError`.

What the thirteen views actually use, and it is the whole set they need —
`grep -ho '@[a-z]*' views.go views_auth_flow.go | sort | uniq -c`:

`@extends` `@section`/`@endsection` `@yield` `@if`/`@endif` `@go`/`@endgo`
`@csrf`. Plus `{{ }}` which escapes, `{!! !!}` which does not, and `{{-- --}}`
which is stripped and never reaches the page. There is no loop in any of them.

Before reaching for a directive that is on neither list, check the compiler
rather than this file: it is the CLI's, and its set is closed.

## Four rules that will bite you

**Never interpolate where an attribute name goes.** An HTML entity is not
decoded in an attribute name, so a value written there is read as syntax. Every
other position has an escaper and this one has none, so it is refused rather
than guarded. A conditional attribute is `@if` around the whole attribute:

```
<input type="checkbox" name="remember" value="1"
	@if(.Remember)
		checked
	@endif
>
```

`TestNoScreenInterpolatesWhereAnAttributeNameGoes` at
`views_internal_test.go:216` holds every screen to it. The kit shipped exactly
one such site — a helper answering `checked` or nothing — and nothing could be
injected through it; it was still wrong to publish, because these screens are
what a project copies.

**`{!! !!}` is for a call, not a value.** It is entitled to skip escaping only
because a component escaped everything it interpolated. `{!! .Status !!}` is
stored cross-site scripting the first time a `Status` comes from a person.

**No Bootstrap class, and no invented one.** The styling is Tailwind utilities
plus the semantic classes the stylesheet ships — `card`, `btn`, `input`,
`field`. A class the stylesheet has never heard of renders as nothing at all,
which looks like a broken build.
`TestTheAuthViewsCarryNoBootstrap` at `views_internal_test.go:166` names the
ones that already got in once: `form-control`, `btn btn-`, `btn-primary`,
`card-body`, `card-header`, `navbar-nav`, `col-md-`, `invalid-feedback`,
`alert-success`.

**Do not type the framework's name into published markup.** The brand is
`.BrandName`, filled from the application's own configuration. The verification
mail once carried the literal word, so every project running this command signed
its first message to its own users with a name that was not theirs.
`TestNothingTheKitPublishesIsBrandedWithItsOwnName` at `flow_internal_test.go:346`
searches every published file for it.

## Message bodies

Both messages are built the same way, and that is enforced. Each goes through
`mailui.Layout` rather than its own `<table>` — one of them was 40 lines of
hand-written table with its own greys and its own footer, which is two designs
from one application landing in the same inbox a week apart. Each also carries a
custom block in kyse comment syntax:

```
{{-- arandu:begin custom --}}
{{-- arandu:end custom --}}
```

That block is the wording a project decided to send its own users, and a
republish carries it over. `TestBothMessagesAreBuiltTheSameWay` at
`flow_internal_test.go:379` fails if a mail view loses it. Both parts of both
messages ship — a mail with no plain-text part is filed as spam more often and
shows nothing in a client that cannot render HTML.

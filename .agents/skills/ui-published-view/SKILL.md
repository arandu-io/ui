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

`AuthViews` in `views.go:110` is the list, and it is the map from constant to
published path. Seventeen views come out of it, plus `page.go` — the struct the
thirteen screens render from — and the two files under
`resources/js/`, which are the one asset the layout asks for by name:

| published path | constant |
| --- | --- |
| `resources/views/layouts/app.kyse.go` | `authLayoutViewTemplate` |
| `resources/js/js.go` | `scriptRegistrationTemplate` |
| `resources/js/custom.js` | `customScriptTemplate` |
| `resources/views/home.kyse.go` | `authHomeViewTemplate` |
| `resources/views/welcome.kyse.go` | `authWelcomeViewTemplate` |
| `resources/views/auth/login.kyse.go` | `authLoginViewTemplate` |
| `resources/views/auth/register.kyse.go` | `authRegisterViewTemplate` |
| `resources/views/auth/verify.kyse.go` | `authVerifyViewTemplate` |
| `resources/views/auth/passwords/confirm.kyse.go` | `authPasswordConfirmViewTemplate` |
| `resources/views/auth/passwords/email.kyse.go` | `authPasswordEmailViewTemplate` |
| `resources/views/auth/passwords/reset.kyse.go` | `authPasswordResetViewTemplate` |
| `resources/views/auth/two-factor/challenge.kyse.go` | `authTwoFactorChallengeViewTemplate` |
| `resources/views/auth/two-factor/recovery.kyse.go` | `authTwoFactorRecoveryViewTemplate` |
| `resources/views/auth/two-factor/setup.kyse.go` | `authTwoFactorSetupViewTemplate` |
| `resources/views/auth/two-factor/recovery-codes.kyse.go` | `authRecoveryCodesViewTemplate` |
| `resources/views/mail/verify-email.kyse.go` | `verifyMailViewTemplate` |
| `resources/views/mail/verify-email-text.kyse.go` | `verifyMailTextTemplate` |
| `resources/views/mail/password-reset.kyse.go` | `passwordMailViewTemplate` |
| `resources/views/mail/password-reset-text.kyse.go` | `passwordMailTextTemplate` |
| `app/Http/Controllers/Auth/page.go` | `authPageTemplate` |

The last four view rows are message bodies from `views_auth_flow.go`:
`verifyMailViewTemplate`,
`verifyMailTextTemplate`, `passwordMailViewTemplate`, `passwordMailTextTemplate`.
They are not screens — no layout, no navigation, no token — and the suite counts
them separately.

## The two levels of template, and this is the one trap

A view constant is rendered twice. First by this program, then by kyse in
somebody's project:

- **`<% %>` is this generator.** `render` in `publish.go:48` switches the
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

**2. Read what the screen is allowed to read.** `AuthPage` in `views.go:213` is
the struct, published to `app/Http/Controllers/Auth/page.go`. It embeds
`view.Page` for the chrome — title, description, token, navigation — and adds
the status line, the address a link carried, the remember-me box, route URLs,
the password policy and two-factor provisioning material. A password box where
a password is chosen passes `Policy: .PasswordPolicy`, the policy its handler
fills from `passwordPolicy` and checks the form against, so the checklist
under the box is the rule that refuses it; a `Confirming` box draws no
checklist and passes none. It has no field per message: a rejected
form comes back through the router, the flash lands in `view.Page.Errors` and
`view.Page.Old`, and a component asks the page by field name —
`components.Field(components.FieldProps{Name: "email", Page: .})` draws the
message and starts from what was typed. A screen that draws a message by hand
asks `.FieldError("authenticator_code")`; there is no `.EmailError` to read.

If a screen needs something new, add the field to `authPageTemplate` **and fill
it in from a handler in the same change.** A URL field read by a template and
assigned by nobody renders `action=""`, which posts to the current URL and looks
like it worked. `TestEveryAddressAScreenReadsIsFilledInSomewhere` in
`flow_internal_test.go:560` fails on either half — read and never filled, filled
and never read — and `TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn` at
`flow_internal_test.go:742` does the same for the messages: every field a
handler rejects with needs an input of that `Name` on some screen, and a notice
needs a screen that draws `.Status`.

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
does not depend on the CLI. `plans/prova-ponta-a-ponta.sh` in the working tree
publishes the kit into a generated project, runs the real `aru view:build`, the
real `go build`, and renders every page over HTTP. That is the only place a
markup error surfaces before somebody else's build.

## What kyse does not have

`TestTheAuthViewsInventNoDirective` at `views_internal_test.go:244` reads every
view and fails on any of these:

`@vite` `@auth` `@guest` `@error` `@can` `@props` `@stack` `@push` `@forelse`
`@switch` `@fonts` `<x-`

`TestTheAuthViewsReachForNoHelper` at `views_internal_test.go:279` fails on
`config(` `route(` `auth()` `__(` `old(` `session(` `Route::has`. Everything a
screen shows came from the handler, in the struct. The guest branch of the
navigation is `@if(!.SignedIn())`; a validation message is asked for by the
component, through `FieldError`.

What the seventeen views actually use, and it is the whole set they need —
`grep -ho '@[a-z]*' views.go views_auth_flow.go | sort | uniq -c`:

`@extends` `@section`/`@endsection` `@yield` `@if`/`@endif` `@go`/`@endgo`
`@csrf`. Plus `{{ }}` which escapes, `{!! !!}` which does not, and `{{-- --}}`
which is stripped and never reaches the page. There is no loop in any of them.

Before reaching for a directive that is on neither list, check the compiler
rather than this file: it is the CLI's, and its set is closed.

## Rules that will bite you

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
`views_internal_test.go:312` holds every screen to it. The kit shipped exactly
one such site — a helper answering `checked` or nothing — and nothing could be
injected through it; it was still wrong to publish, because these screens are
what a project copies.

**`{!! !!}` accepts only a `template.HTML`, and that is a compile rule.** The
view compiler assigns the value to a `template.HTML` before writing it, so a
component call compiles and `{!! .Status !!}` with a `string` field stops the
build at the line of the screen. A component is entitled to skip escaping only
because it escaped everything it interpolated. Never convert a string to
`template.HTML` to make a screen build: these screens are what a project
copies, and a converted `Status` is stored cross-site scripting the first time
one comes from a person.

Every `{!! !!}` in a published view is a component's, and that includes the
two-factor QR code, which is not raw output at all. The setup screen draws it
as `<img src="data:image/svg+xml;base64,{{ .QRCodeImage() }}">`: the screen
spells out the scheme, because the view escape refuses an address whose scheme
comes from a value, and `AuthPage.QRCodeImage` answers the base64 body. The SVG
is drawn as an image and never run, and nothing in `page.go` names
`html/template` or `template.HTML`.
`TestTheQRCodeIsAnImageAndNoPublishedGoImportsHTMLTemplate` holds all three.

**No Bootstrap class, and no invented one.** The styling is Tailwind utilities
plus the semantic classes the stylesheet ships — `card`, `btn`, `input`,
`field`. A class the stylesheet has never heard of renders as nothing at all,
which looks like a broken build.
`TestTheAuthViewsCarryNoBootstrap` at `views_internal_test.go:262` names the
ones that already got in once: `form-control`, `btn btn-`, `btn-primary`,
`card-body`, `card-header`, `navbar-nav`, `col-md-`, `invalid-feedback`,
`alert-success`.

**Every screen is answered whole.** Four things in a published page can hold
state — a component, the layout, the screen, and a fragment a swap puts inside
it — and what tells them apart is when each is next drawn. The layout runs once
per document and no swap redraws it; a screen is answered whole or not at all; a
fragment is what is inside one swap target.

This kit publishes no fragment. A direct visit, a boosted link and a history
restore each get the whole screen, and so does a rejected form: it goes back
through a redirect and the screen is drawn again, message and typed input
included. So no form here carries `hx-target` or `hx-swap` — the body's
`hx-boost` is all the htmx a form needs — and the layout leaves htmx's response
handling at its default, with `includeIndicatorStyles:false` because the policy
refuses the `<style>` htmx would inject. Teaching it to swap a 422 would be a
second way to answer the same rejection.
`TestTheKitPublishesNoFragmentAndAsksForNone` refuses a view under `partials/`
and published Go that calls `.Fragment(`, and
`TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne` refuses a narrowed swap
on a screen. A screen that comes to need a piece of itself back brings the two
gates that held the old sign-in fragment to its swap target back from history:
`TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap` and
`TestNothingTheLayoutDrawsIsRedrawnInsideASwap`.

**No `x-data`, and none of its relatives.** State on this stack is the server's.
Nothing the layout loads reads such an attribute, and nothing could — the policy
is `script-src 'self'` with no `unsafe-eval`, so a framework compiling a
directive out of a string throws before it runs. What dies with the tab is
`ui.js`'s: it binds on `document`, dispatches on `data-` attributes and keeps
open and selected in the ARIA the markup already carries, so the DOM holds the
only copy. `TestNoPublishedViewKeepsStateInTheBrowser` in
`publish_internal_test.go` reads every published view for one.

**The layout's head is not yours to shrink.** It is in `replaced`, so publishing
overwrites the skeleton's with no flag at all — an element only the skeleton had
is one every project loses silently.
`TestTheKitsLayoutKeepsWhatTheSkeletonsLayoutCarries` compares the two heads
element by element and skips when `../arandu` is not checked out.

**A tag you add to the head is a requirement on every project that receives it.**
`view.URL` panics on a name nothing registered — deliberately, because a
plausible URL for a file the binary does not carry is a 404 on every page that
nobody reads. Five names are embedded by the runtime and need nothing:
`app.css`, `htmx.min.js`, `ui.js`, `basecoat.bundle.js` and `theme.js`. Any
other name has to be published by this kit with the package that registers it,
the way `custom.js` is published with `resources/js/js.go`.
`TestEveryAssetAPublishedViewAsksForIsOneSomethingRegisters` reads every
published view for one, and it exists because the layout shipped the `custom.js`
tag with nothing behind it: every project older than that day's skeleton
received a layout that panicked on every request, and the layout is in
`replaced`, so nobody chose it.

**Do not type the framework's name into published markup.** The brand is
`.BrandName`, filled from the application's own configuration. The verification
mail once carried the literal word, so every project running this command signed
its first message to its own users with a name that was not theirs.
`TestNothingTheKitPublishesIsBrandedWithItsOwnName` at `flow_internal_test.go:634`
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
`flow_internal_test.go:668` fails if a mail view loses it. Both parts of both
messages ship — a mail with no plain-text part is filed as spam more often and
shows nothing in a client that cannot render HTML.

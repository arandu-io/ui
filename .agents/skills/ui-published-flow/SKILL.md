---
name: ui-published-flow
description: Change a handler, a route, a mailable or a constructor that the Arandu starter kit publishes into somebody's project. Use when the request is to "add a route to the kit", "change the login handler", "add a step to registration", "change the password reset flow", "fix the redirect", "change the verification email link", "add a parameter to the constructor", or when a pull request touches views_controllers.go, views_auth_flow.go or the published LoginController, RegisterController, PasswordController or HomeController. Covers the 14 routes and the exact table that names them, what a rejected form must answer, the signed links that are stored nowhere, and the two checks that stop a republish from breaking somebody's build.
license: MIT
---

# Changing the flow the kit publishes

The nine Go files are what makes thirteen views a flow rather than thirteen
pages. They land in `app/Http/Controllers/Auth/`, `app/Http/Controllers/` and
`app/Mail/`, and from that moment they are the project's: the minimum password
length, whether registration is open, what a confirmed address may do — all of
it is a line somebody can read and change in their own repository.

They are string constants here. `GenerateAuth` in `views_controllers.go:18` is
the list; the constants are spread across `views_controllers.go` and
`views_auth_flow.go`.

**Two of the four controllers carry no custom block, and the command says
otherwise.** Measured on a published tree, only `LoginController.go` and
`LoginController_handlers.go` under `app/Http/Controllers/Auth/` contain
`// arandu:begin custom`; `RegisterController.go`, `PasswordController.go`,
`render.go` and `page.go` contain none
(`grep -c 'arandu:begin custom' app/Http/Controllers/Auth/*.go`). The `wiring`
text this command prints says the opposite — that the minimum password length
and whether registration is open are *"inside a custom block that survives a
--force"* — and the minimum length is `security.MinPasswordLen`, read in
`PasswordController.go`, in no block at all. Without `--force` those two files
are kept whole, so an edit is safe; with `--force` it is gone. An edit appended
to a published `RegisterController.go` did not survive `auth --force`. Either
add the blocks or change the text, but do not repeat the claim.

## The 14 routes, and the table that owns their names

In the order they are registered, which is the order the test knows. The paths
are relative to the group, and resolve under `/auth`:

```
 1  Get   /login              auth.login
 2  Post  /login              —
 3  Post  /logout             auth.logout
 4  Get   /password           auth.password.request
 5  Post  /password/email     auth.password.email
 6  Get   /password/reset     auth.password.reset
 7  Post  /password/update    auth.password.update
 8  Get   /password/confirm   auth.password.confirm
 9  Post  /password/confirm   —
10  Get   /register           auth.register
11  Post  /register           —
12  Get   /verify             auth.verify.notice
13  Get   /verify/confirm     auth.verify.confirm
14  Post  /verify/resend      auth.verify.resend
```

A POST that shares its address with the GET beside it is left unnamed, which is
why three have no name: the path built from the GET's name is already where the
form posts, and a second name for one address is a choice nobody can make
correctly.

`TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy` at
`flow_internal_test.go:940` holds this table **exactly, order included**. A
route added to the kit is therefore a row somebody wrote there, rather than a
screen that quietly arrives unnamed.

This module replaces the framework's auth module, so the names that module gave
have to keep resolving to the same addresses: `auth.login` to `/auth/login` and
`auth.logout` to `/auth/logout`, read off the framework's own router rather than
restated. `TestTheNamesSurviveTheSubstitution` at `flow_internal_test.go:991`
compiles the published Go into a module of its own and asks. It also checks the
two the guards redirect to — `auth.login` against `middleware.SignInPath`,
`auth.password.confirm` against `middleware.PasswordConfirmPath` — because a
screen the guards cannot reach is a redirect to a 404. It skips when the sibling
checkouts are absent: this module is released alone, and its CI has only itself.

## The procedure

**1. Change the constant.** Keep every published symbol commented: the reference
on pkg.go.dev of the *project that receives this* is generated from those
comments.

**2. If a screen reads something new, add the field to `authPageTemplate` and
fill it in here, in the same change.** A URL field read by a template and
assigned by nobody renders `action=""`, which posts to the current URL and looks
like it worked. `TestEveryAddressAScreenReadsIsFilledInSomewhere` at
`flow_internal_test.go:272` fails on either half.

**3. If you added a route, add its row to the table** at
`flow_internal_test.go:940`, and make sure something draws the screen it serves.
`TestEveryScreenThisKitPublishesIsDrawnBySomething` at
`flow_internal_test.go:664` parses every published Go file and requires each
view to be named by one. The landing page shipped once with the Login and
Register buttons on it, reachable by nothing, while the dashboard was drawn for
guests and signed-in people alike.

**4. Gates, then goldens.**

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go') \
  && go build ./... && go vet ./... && go test -race ./... \
  && bash tests/test-layout-guard.sh
go test . -update && git diff testdata
```

`go build ./...` passing here says nothing about the published Go: it is a
string. Two different things read it, and only one of them is a compiler.

It **parses** in `TestTheGeneratedGoParses` at `publish_internal_test.go:68` and
in `render` at `publish.go:44`, which runs `format.Source` over every non-view
file and fails generation with *this is a bug in this generator*. Parsing is not
enough and never was: a file that calls a method nobody declares parses
perfectly. `render.go` shipped calling `auth.Service.Names` after the method had
been renamed to `PublicNames`, and every project that published this kit got a
file that does not build.

It **compiles** in
`TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework` at
`publish_internal_test.go:797`, which lays the nine Go files into a throwaway
module requiring the framework by published tag — no `replace`, so it is the
framework a person receives and not the checkout beside this one — and runs
`go build`. That is the test that fails when a handler calls a symbol the
framework no longer has, or when a call is added without its import: the
published templates carry import blocks of their own.

## What the published handlers must keep doing

**A rejected form is never answered 200.** HTMX swaps the fragment of a 422 and
of a 200 alike, so answering 200 to a forged link leaves the browser, the log
and every proxy believing it worked. `screenStatus` exists for that, and
`TestARejectedFormIsNeverAnswered200` at `flow_internal_test.go:404` checks it.

**The redirect survives without JavaScript.** Every form carries `method="post"`
and `action=` as well as `hx-post`, because both scripts are deferred and may
never arrive. A handler must go through `http.Redirect(w, r, to)`, which answers
`HX-Redirect` under HTMX and a 303 with a `Location` otherwise. Setting the
header directly gives a plain browser form post 200 and an empty body — a blank
page. `TestTheRedirectSurvivesWithoutJavaScript` at
`publish_internal_test.go:588` checks both exits.

**Both links are signed, and stored nowhere.** Verification and password reset
carry a signed token rather than a row, so they survive a restart and a second
replica, and the reset link stops working the moment the password changes —
there is no table of tokens to sweep. `TestTheResetLinkIsSignedAndHeldNowhere`
at `flow_internal_test.go:53` and
`TestNothingIsConsumedUntilThePasswordIsAcceptable` at
`flow_internal_test.go:118` are the two that keep it.

**The reset says the same thing either way.** *If that address is registered, a
link is on its way.* — whether it is or not, and nothing is mailed to an address
nobody looked up. `flow_internal_test.go:146` and `:74`.

**Neither token survives being serialized.** Two reach `AuthPage`: the session's
CSRF token through the embedded `view.Page`, and the one-time token of a reset
link. A type that holds either and serializes itself whole is one debug dump
away from publishing it, so `MarshalJSON` and `LogValue` are written by hand.
`TestNeitherTokenSurvivesBeingSerialized` at `flow_internal_test.go:793`.

**The tenant does not come from the request body**, and the kit does not
migrate. `publish_internal_test.go:188` and `:204`.

## Changing a constructor is the one that breaks strangers

`HomeController.go` is in `replaced`: publishing overwrites it with **no flag at
all**. So the moment the kit's constructor stops matching the one a project's
`bootstrap/app.go` calls, publishing into that project breaks its build — and
neither repository notices, because each compiles on its own. This shipped: three
parameters emitted, five passed.

Two checks stand there, and both must pass before a signature changes:

- `TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes` at
  `publish_internal_test.go:525` — the printed instruction against the emitted
  constructor, for `controllers.NewHomeController` and `authui.New`.
- `TestTheProjectsInThisTreeCompileTheConstructorTheKitPublishes` at
  `publish_internal_test.go:555` — the emitted constructor against
  `../arandu` and `../examples`. It **skips** when they are not checked out
  beside this module, so a green run on a machine with only this repository
  proves nothing about it. Check the siblings out before changing a signature.

When one does change, the `wiring` constant in `main.go:187` changes with it, in
the same commit.

## Two things the published code says about itself that are worth knowing

`arandu.mod.toml` declares this kit `filesystem = true` and the other three
false — writing into a project is the whole job, and it neither reaches the
network, runs a program, nor owns a table.

The two messages go out through the project's own mailer. In development that is
`MAIL_URL=log://`, so the links land in the output of `aru dev` and the whole
flow works with nothing installed. Both messages are built from `mailui` rather
than hand-written tables, and both carry the wording in a custom block that a
republish preserves.

---
name: ui-published-flow
description: Change a handler, a route, a mailable or a constructor that the Arandu starter kit publishes into somebody's project. Use when the request is to "add a route to the kit", "change the login handler", "add a step to registration", "change the password reset flow", "fix the redirect", "change the verification code", "add a parameter to the constructor", or when a pull request touches views_controllers.go, views_auth_flow.go or the published LoginController, RegisterController, PasswordController, TwoFactorController or HomeController. Covers the 23 routes, including the 9 two-factor routes, the exact table that names them, what a rejected form must answer, the purpose-bound native codes, and the two checks that stop a republish from breaking somebody's build.
license: MIT
---

# Changing the flow the kit publishes

The ten plain Go files are what make eighteen views a flow rather than a set of
pages. They land in `app/Http/Controllers/Auth/`, `app/Http/Controllers/` and
`app/Mail/`, and from that moment they are the project's: the minimum password
length, whether registration is open, what a confirmed address may do — all of
it is a line somebody can read and change in their own repository.

They are string constants here. `GenerateAuth` in `views_controllers.go:18` is
the list; the constants are spread across `views_controllers.go` and
`views_auth_flow.go`.

**Two of the five authentication controller files carry a custom block.**
`LoginController.go` and `LoginController_handlers.go` contain
`// arandu:begin custom`; `RegisterController.go`, `PasswordController.go` and
`TwoFactorController.go` do not. A normal republish keeps all five whole. With
`--force`, generated security fixes replace the required flow and only content
inside a marked block is carried over. Across the complete publication, 9 of 28
files carry a block: 5 use Go comments and 4 message bodies use kyse comments.

## The 23 routes, and the table that owns their names

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
13  Post  /verify/confirm     auth.verify.confirm
14  Post  /verify/resend      auth.verify.resend
15  Get   /two-factor/challenge              auth.two-factor.challenge
16  Post  /two-factor/challenge              —
17  Get   /two-factor/recovery               auth.two-factor.recovery
18  Post  /two-factor/recovery               —
19  Get   /two-factor/setup                  auth.two-factor.setup
20  Post  /two-factor/setup                  —
21  Post  /two-factor/setup/confirm          auth.two-factor.setup.confirm
22  Post  /two-factor/disable                auth.two-factor.disable
23  Post  /two-factor/recovery-codes         auth.two-factor.recovery-codes
```

A POST that shares its address with the GET beside it is left unnamed, which is
why six have no name: the path built from the GET's name is already where the
form posts, and a second name for one address is a choice nobody can make
correctly.

`TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy` at
`flow_internal_test.go:946` holds this table **exactly, order included**. A
route added to the kit is therefore a row somebody wrote there, rather than a
screen that quietly arrives unnamed.

The application-owned module preserves the established authentication names:
`auth.login` resolves to `/auth/login` and `auth.logout` to `/auth/logout`.
`TestTheNamesSurviveTheSubstitution` at `flow_internal_test.go:1007`
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
`flow_internal_test.go:262` fails on either half.

**3. If you added a route, add its row to the table** at
`flow_internal_test.go:946`, and make sure something draws the screen it serves.
`TestEveryScreenThisKitPublishesIsDrawnBySomething` at
`flow_internal_test.go:655` parses every published Go file and requires each
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

It **parses** in `TestTheGeneratedGoParses` at `publish_internal_test.go:92` and
in `render` at `publish.go:44`, which runs `format.Source` over every non-view
file and fails generation with *this is a bug in this generator*. Parsing is not
enough and never was: a file that calls a method nobody declares parses
perfectly. `render.go` shipped calling `auth.Service.Names` after the method had
been renamed to `PublicNames`, and every project that published this kit got a
file that does not build.

It **compiles** in
`TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework` at
`publish_internal_test.go:802`, which lays the ten Go files into a throwaway
module requiring the framework by published tag — no `replace`, so it is the
framework a person receives and not the checkout beside this one — and runs
`go build`. That is the test that fails when a handler calls a symbol the
framework no longer has, or when a call is added without its import: the
published templates carry import blocks of their own.

## What the published handlers must keep doing

**A rejected form is never answered 200.** HTMX swaps the fragment of a 422 and
of a 200 alike, so answering 200 to a forged link leaves the browser, the log
and every proxy believing it worked. `screenStatus` exists for that, and
`TestARejectedFormIsNeverAnswered200` at `flow_internal_test.go:395` checks it.

**The redirect survives without JavaScript.** Every form carries `method="post"`
and `action=` as well as `hx-post`, because both scripts are deferred and may
never arrive. A handler must go through `http.Redirect(w, r, to)`, which answers
`HX-Redirect` under HTMX and a 303 with a `Location` otherwise. Setting the
header directly gives a plain browser form post 200 and an empty body — a blank
page. `TestTheRedirectSurvivesWithoutJavaScript` at
`publish_internal_test.go:589` checks both exits.

**Verification and reset use purpose-bound native codes.** Both flows issue and
consume through the application's `onetime.CodeStore`; the purpose and subject
keep a code from crossing flows or accounts, and consumption is single-use and
atomic. `TestTheResetUsesOnlyPurposeBoundNativeCodes` at
`flow_internal_test.go:47` and `TestNothingIsConsumedUntilThePasswordIsAcceptable`
at `flow_internal_test.go:108` keep the password flow on that boundary.

**What the sign-up form asks for is a setting, not a shape.** `registrationAsks`
in `RegisterController.go` is one of `PasswordTwice` (the zero value, and what
ships), `PasswordOnce` or `NoPassword`. One value feeds both sides: the handler
gates each password rule on it, and the screen draws each password box inside an
`@if` on `AuthPage.AsksForPassword()` / `AsksForPasswordConfirmation()`, which
the handler fills from the same value. Switching it off in one of the two and
not the other is the defect this replaced, inverted — a rule on an input nobody
can see rejects every submission and hangs the message on a field that is not on
the page.

Those two are **methods over negated fields** — `WithoutPasswordBox` and
`WithoutConfirmationBox` — and the negation is the mechanism, not a taste.
`page.go` is in `replaced` and `RegisterController.go` is not, so
`auth --views --force` writes a new sign-up screen beside a handler that
predates the setting and fills neither field. False has to mean *draw the box*,
because false is what that handler leaves behind; spelled the positive way, the
safe flag would quietly stop asking for a password.
`TestTheSignUpFormAsksForAPasswordTwiceUnlessTheProjectSaysOtherwise` holds the
default, `TestTheSignUpFormAndItsHandlerAskForTheSameThing` holds the two ends
together, and `TestASignUpScreenNobodyFilledInStillAsksForBothBoxes` holds the
negation.

`NoPassword` hands the credential to `Users.Register`, and the requirement on
that implementation is one line: do not hash the empty password it is given. A
stored hash of the empty string is a credential anybody can offer. An account
with no password is one whose password column is EMPTY, which the native
provider refuses to authenticate whatever is offered against it. On the near
side, `doLogin`, `confirmPassword` and `updatePassword` each refuse an empty
password before the call that compares it, and `doRegister` clears a password
the form did not draw before calling `Register` — so no path here puts an empty
value into a comparison.
`TestNoPublishedHandlerPutsAnEmptyPasswordIntoAComparison` reads all four.

**The reset says the same thing either way.** *If that address is registered, a
code is on its way.* — whether it is or not, and nothing is mailed to an address
nobody looked up. `flow_internal_test.go:136` and `:67`.

**Provisioning material and CSRF do not survive serialization.** `AuthPage`
carries the session's CSRF token plus the authenticator secret, QR markup and
recovery codes. A type that serializes itself whole is one debug dump away from
publishing them, so `MarshalJSON` and `LogValue` are written by hand.
`TestNeitherTokenSurvivesBeingSerialized` at `flow_internal_test.go:796`.

**The tenant does not come from the request body**, and the kit does not
migrate. `publish_internal_test.go:215` and `:231`.

## Changing a constructor is the one that breaks strangers

`HomeController.go` is in `replaced`: publishing overwrites it with **no flag at
all**. So the moment the kit's constructor stops matching the one a project's
`bootstrap/app.go` calls, publishing into that project breaks its build — and
neither repository notices, because each compiles on its own. This shipped: three
parameters emitted, five passed.

Two checks stand there, and both must pass before a signature changes:

- `TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes` at
  `publish_internal_test.go:522` — the printed instruction against the emitted
  constructor, for `controllers.NewHomeController` and `authui.New`.
- `TestTheProjectsInThisTreeFitTheConstructorTheKitPublishes` at
  `publish_internal_test.go:556` — the emitted constructor against the sibling
  `../arandu` skeleton. It **skips** when that checkout is not beside this
  module, so a green run on a machine with only this repository proves nothing
  about it. Check the sibling out before changing a signature.

When one does change, the `wiring` constant in `main.go:219` changes with it, in
the same commit.

## Two things the published code says about itself that are worth knowing

`arandu.mod.toml` declares this kit `filesystem = true` and the other three
false — writing into a project is the whole job, and it neither reaches the
network, runs a program, nor owns a table.

The two messages go out through the project's own mailer. In development that is
`MAIL_URL=log://`, so the codes land in the output of `aru dev` and the whole
flow works with nothing installed. Both messages are built from `mailui` rather
than hand-written tables, and both carry the wording in a custom block that a
republish preserves.

---
name: ui-published-flow
description: Change a handler, a route, a mailable or a constructor that the Arandu starter kit publishes into somebody's project. Use when the request is to "add a route to the kit", "change the login handler", "add a step to registration", "change the password reset flow", "fix the redirect", "change the verification code", "add a parameter to the constructor", or when a pull request touches views_controllers.go, views_auth_flow.go or the published LoginController, RegisterController, PasswordController, TwoFactorController or HomeController. Covers the 23 routes, including the 9 two-factor routes, the exact table that names them, what a rejected form must answer, the purpose-bound native codes, and the two checks that stop a republish from breaking somebody's build.
license: MIT
---

# Changing the flow the kit publishes

The ten plain Go files are what make seventeen views a flow rather than a set of
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
inside a marked block is carried over. Across the complete publication, 9 of 29
files carry a block: 5 use Go comments and 4 message bodies use kyse comments.

## The 23 routes, and the table that owns their names

In the order they are registered, which is the order the test knows. The paths
are relative to the group, and resolve under `/auth`. The last column marks the
ten registered with `Action`: their handlers read a form, so they are
`func(*hhttp.Context) error`, convert the form with `ctx.Bind`, and return a
refusal to the router.

```
 1  Get   /login              auth.login
 2  Post  /login              —                                 Action
 3  Post  /logout             auth.logout
 4  Get   /password           auth.password.request
 5  Post  /password/email     auth.password.email               Action
 6  Get   /password/reset     auth.password.reset
 7  Post  /password/update    auth.password.update              Action
 8  Get   /password/confirm   auth.password.confirm
 9  Post  /password/confirm   —                                 Action
10  Get   /register           auth.register
11  Post  /register           —                                 Action
12  Get   /verify             auth.verify.notice
13  Post  /verify/confirm     auth.verify.confirm               Action
14  Post  /verify/resend      auth.verify.resend                Action
15  Get   /two-factor/challenge              auth.two-factor.challenge
16  Post  /two-factor/challenge              —                  Action
17  Get   /two-factor/recovery               auth.two-factor.recovery
18  Post  /two-factor/recovery               —                  Action
19  Get   /two-factor/setup                  auth.two-factor.setup
20  Post  /two-factor/setup                  —
21  Post  /two-factor/setup/confirm          auth.two-factor.setup.confirm   Action
22  Post  /two-factor/disable                auth.two-factor.disable
23  Post  /two-factor/recovery-codes         auth.two-factor.recovery-codes
```

A POST that shares its address with the GET beside it is left unnamed, which is
why six have no name: the path built from the GET's name is already where the
form posts, and a second name for one address is a choice nobody can make
correctly.

`TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy` at
`flow_internal_test.go:1226` holds this table **exactly, order included, the
last column too**. A route added to the kit is therefore a row somebody wrote
there, rather than a screen that quietly arrives unnamed — and a route that
moved from `Action` back to `Post` is a handler that would have to draw its own
refusal.

The application-owned module preserves the established authentication names:
`auth.login` resolves to `/auth/login` and `auth.logout` to `/auth/logout`.
`TestTheNamesSurviveTheSubstitution` at `flow_internal_test.go:1294`
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
`flow_internal_test.go:560` fails on either half.

**3. If you added a route, add its row to the table** of
`TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy` at
`flow_internal_test.go:1226`, and make sure something draws the screen it serves.
`TestEveryScreenThisKitPublishesIsDrawnBySomething` at
`flow_internal_test.go:899` parses every published Go file and requires each
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

It **parses** in `TestTheGeneratedGoParses` at `publish_internal_test.go:96` and
in `render` at `publish.go:48`, which runs `format.Source` over every non-view
file and fails generation with *this is a bug in this generator*. Parsing is not
enough and never was: a file that calls a method nobody declares parses
perfectly. `render.go` shipped calling `auth.Service.Names` after the method had
been renamed to `PublicNames`, and every project that published this kit got a
file that does not build.

It **compiles** in
`TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework` at
`publish_internal_test.go:1019`, which lays the ten Go files into a throwaway
module requiring the framework by published tag — no `replace`, so it is the
framework a person receives and not the checkout beside this one — and runs
`go build`. That is the test that fails when a handler calls a symbol the
framework no longer has, or when a call is added without its import: the
published templates carry import blocks of their own.

## What the published handlers must keep doing

**A form is bound, never read by hand.** A handler that reads a form declares
a request struct beside it — `loginRequest`, `registrationRequest`,
`passwordUpdateRequest` and the rest — with a `form:"…"` tag per field and a
`LogValue` that says which fields arrived and nothing they carried, and fills
it with `ctx.Bind(&in)`, returning the error as it came. A key the struct does
not declare reaches no field, an unticked box is `false`, and every value
arrives trimmed but a password, which arrives as typed from hesape v0.50.2 on.
No handler calls `PostFormValue`,
`FormValue`, `ParseForm` or `Validate()`; the checks a form needs are written
in the handler, where they were. `aru doctor` reports the first shape as
`input-read-by-hand` and the second as `validate-called-by-controller`, and
`TestNoPublishedHandlerReadsItsFormByHand` refuses both, and an `Action` whose
handler does not bind.

**A new password is held to one policy.** `passwordPolicy` in
`RegisterController.go` is the rule — from `hashing.MinPasswordLen` to
`hashing.MaxPasswordLen` — and both ends read it: `showRegister` and
`showPasswordReset` hand it to the screen as `AuthPage.PasswordPolicy`, which
the password box draws its checklist from, and `doRegister` and
`updatePassword` check against it through `checkNewPassword`. Left nil, the box
draws `validation.PasswordDefault`, a minimum of eight, and the handler refused
twelve: the checklist ticked every line and the form was turned away.
`checkNewPassword` refuses the empty password before it asks the policy,
because the policy's length rule passes a value that is absent.
`TestTheChecklistAndTheHandlerAreOnePasswordPolicy` holds the four ends.

**A rejected form is returned, never drawn.** A handler that refuses a form
returns `validation.Errors` — `return validation.Errors{"email": {"…"}}`, or the
`errs` it built when `errs.Any()` — and the router answers it: a 303 back to the
Referer for a page, marked `no-store`, with the messages and what was typed in
the flash and every secret dropped; `HX-Redirect` with no body for htmx, which
follows it as a navigation; a 422 problem document with the messages by field
for a client that asked for JSON. The handler writes no status and renders
nothing. The screen it lands on is drawn whole by `Module.screen`, which puts
the flash on the page — messages in `view.Page.Errors`, typed input in
`view.Page.Old` — so the components draw both by field name. The key of each
message is the `Name` of an input on the screen the form is on; a key with no
input is carried back and dropped.

Only an `Action` has a router to return to and a context to bind from, so a
handler that reads a form is registered with `g.Action(stdhttp.MethodPost, …)`
and takes the response and request off the context (`w, r := ctx.Response,
ctx.Request`). `Routes` writes the module's notices with the
router's flash when the router carries one, as the kernel's does, and hands a
router that carries none — one a test builds — the flash `New` made, so a
rejection and a notice always travel in one cookie with one key and one Secure
attribute. It asks through the `flashCarrier` interface rather than calling
`r.Flash()`, so the file still compiles against a framework older than v0.53.0,
where the module wires its own flash as before.
`TestTheKitWritesItsNoticesWithTheRoutersFlash` tells the two apart by key and
by Secure. A status a refusal carries goes on the
header before returning — the sign-in lock sets `Retry-After` and returns the
message. `TestARejectedFormIsReturnedToTheRouter` refuses a published Go file
that writes a refusal status, and
`TestARejectedSignInGoesBackToTheFormInEveryTransport` runs the three answers.

**A screen after a successful form is reached by a redirect.** The reset code
sent, the address confirmed, the password changed, a code resent: each goes
through `Module.notify(w, r, to, notice, old)`, which leaves the sentence and
the address in the flash and redirects. A screen drawn as the body of a POST is
one a refusal cannot be sent back to — the Referer is the POST's address, and a
GET there is a 405 — and one a reload posts again.

The one place this costs something is two-factor setup. A wrong confirmation
code goes back to the setup screen, and the secret is not carried across the
redirect — it would have to travel in a cookie — so the screen offers to start
again, and starting again issues a new one.

**The redirect survives without JavaScript.** Every form carries `method="post"`
and `action=`, and the body's `hx-boost` is all the htmx a form here needs; both
scripts are deferred and may never arrive. A handler must go through
`redirect(w, r, to)`, which calls `http.Redirect` from hesape and answers
`HX-Redirect` under HTMX and a 303 with a `Location` otherwise. Setting the
header directly gives a plain browser form post 200 and an empty body — a blank
page. `TestTheRedirectSurvivesWithoutJavaScript` at
`publish_internal_test.go:806` checks both exits.

**A password stored trimmed signs in once more.** Bind trimmed the password
until hesape v0.50.2, so one chosen with spaces at its ends through the v0.21.0
handlers was hashed without them. `doLogin` asks `Users.VerifyCredentials` with
the password as typed; only when that is a plain refusal — not a lock, which is
why `wrongPassword` rules out a `retryAfterError` that also matches
`ErrInvalidCredentials` — and the typed password differs from a non-empty
trimmed form does it call `verifyTrimmedPassword`. That compares the trimmed
form against the hash `Users.Lookup` returns, or a decoy when no account with a
password answers, inside a timebox as long as the service's, so the refusal
takes as long either way. On success it stores the password as typed through
`Users.ResetPassword`, bound to the fingerprint of the hash it compared, and the
sign-in goes on with the rewritten account — so the pending two-factor cookie
carries the new fingerprint. It is one attempt: the service counted the typed
form and nothing here counts again. The refusal is the usual one. Its doc
comment says when to delete it, and
`TestAPasswordStoredTrimmedSignsInOnceAndIsStoredAsTyped` runs every branch.

**Verification and reset use purpose-bound native codes.** Both flows issue and
consume through the application's `onetime.CodeStore`; the purpose and subject
keep a code from crossing flows or accounts, and consumption is single-use and
atomic. `TestTheResetUsesOnlyPurposeBoundNativeCodes` at
`flow_internal_test.go:338` and `TestNothingIsConsumedUntilThePasswordIsAcceptable`
at `flow_internal_test.go:399` keep the password flow on that boundary.

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

**Every second-factor code goes through the account's attempt budget.** The
challenge handlers hand each authenticator code to
`m.factors.VerifyAuthenticator` and each recovery code to
`m.factors.ConsumeRecovery`, and nothing else checks a code. The application's
service counts both kinds against one budget per tenant and user — five codes
per fifteen minutes, counted before the code is checked — because a pending
sign-in lives for five minutes, a wrong code does not spend it, and whoever
holds the password can start as many as they like. A handler that validated a
code itself, through `otp` or `2fa` directly, or that cleared the count when it
issued a new pending sign-in, would hand back unlimited guesses. A spent budget
refuses the right code too, with an error that carries `Seconds()` and also
matches `twofactor.ErrInvalidCode`, so `challengeLocked` checks for it **before**
the wrong-code branch: it clears the pending cookie, sets `Retry-After`, leaves
*Too many codes. Sign in again in N minutes.* in the flash and redirects to the
sign-in screen, which draws it as its status line. Answered as a wrong code, the
person would be sent back to the challenge with a live pending cookie, to be
refused on every try.
`TestALockedChallengeSendsThePersonBackToSignIn` runs both challenge screens
against a fake service returning a lock and a wrong code.

**The reset says the same thing either way.** *If that address is registered, a
code is on its way.* — whether it is or not, and nothing is mailed to an address
nobody looked up.
`TestTheResetSaysTheSameThingWhetherTheAddressIsRegisteredOrNot` at
`flow_internal_test.go:427` and `TestNothingIsMailedToAnAddressNobodyLookedUp` at
`flow_internal_test.go:358`.

**Provisioning material and CSRF do not survive serialization.** `AuthPage`
carries the session's CSRF token plus the authenticator secret, the QR code and
recovery codes. The QR code is an image: the setup screen spells out the
`data:image/svg+xml;base64,` scheme and `AuthPage.QRCodeImage` answers the
body, so no published Go names `html/template` or `template.HTML`. A type that serializes itself whole is one debug dump away from
publishing them, so `MarshalJSON` and `LogValue` are written by hand.
`TestNeitherTokenSurvivesBeingSerialized` at `flow_internal_test.go:1040`.

**The tenant does not come from the request body**, and the kit does not
migrate. `TestTheTenantDoesNotComeFromTheRequestBody` at
`publish_internal_test.go:227` and `TestTheStarterKitDoesNotMigrate` at
`publish_internal_test.go:243`.

## Changing a constructor is the one that breaks strangers

`HomeController.go` is in `replaced`: publishing overwrites it with **no flag at
all**. So the moment the kit's constructor stops matching the one a project's
`bootstrap/app.go` calls, publishing into that project breaks its build — and
neither repository notices, because each compiles on its own. This shipped: three
parameters emitted, five passed.

Two checks stand there, and both must pass before a signature changes:

- `TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes` at
  `publish_internal_test.go:647` — the printed instruction against the emitted
  constructor, for `controllers.NewHomeController` and `authui.New`.
- `TestTheWiringPassesTheOneSecureDecision` — the last argument of the printed
  `authui.New` is `cfg.Framework.Session.Secure`, the value the session cookie
  and the kernel's flash follow, and not the skeleton's own reading.
- `TestTheProjectsInThisTreeFitTheConstructorTheKitPublishes` at
  `publish_internal_test.go:773` — the emitted constructor against the sibling
  `../arandu` skeleton. It **skips** when that checkout is not beside this
  module, so a green run on a machine with only this repository proves nothing
  about it. Check the sibling out before changing a signature.

When one does change, the `wiring` constant in `main.go:250` changes with it, in
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

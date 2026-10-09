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
ten Go files into a throwaway module that requires the framework and the
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

## What it refuses to publish into

`auth` reads the `[arandu] aru` line of the project's `arandu.toml` before it
writes anything, and refuses a project whose floor is below `aruFloor` in
`publish.go` — today `v0.35.0`, measured by publishing into a copy of the
skeleton and compiling the complete generated view set with released CLIs.
`v0.34.0` and below compile `view.Page` through the former framework alias, so
they reject the application-owned native page imported from `hesape/view`.
`v0.35.0` emits the native page contract and compiles all seventeen views,
including the four two-factor screens.

The floor and not the `aru` on PATH. `arandu.mod.toml` declares `exec = false`,
so this module runs nothing; and a CLI built from source or installed with `go
install` reports `dev`, which is every CLI anyone working on this project has.
What the floor answers is the durable question — `aru view:build` reads it
first and refuses a CLI below it, so a floor set too low switches off the one
mechanism that would have said "your aru is old" instead of sixty messages
about markup that is correct.

It also refuses a project whose flow still draws its own refusals.
`checkFlowAnswersByRedirect` reads the flow files this run will not write — all
of them without `--force`, and all of them under `--views`, which never writes
the flow — and refuses when one still names `http.StatusUnprocessableEntity`.
Those are handlers from an earlier kit: beside the layout and `page.go` a run
replaces, a refused sign-in under htmx would be thrown away and the project
would stop building. The message names the files and `auth --force`.

And it refuses a project that keeps a file naming what `page.go` no longer
declares. `checkKeptFilesNameOnlyWhatPageDeclares` reads every published file
this run keeps and refuses when one still names a name in `gonePageNames` —
today `TrustedQRCode`, which the setup screen of v0.20.0 and earlier calls.
`page.go` is replaced on every run, so beside it that screen would stop the
project building. The message names the files and `auth --views --force`.

`--dry-run` is exempt from all three, and the split is the point: it is asked what
would be written and answers that, which is why the counts below still measure
against `../arandu`; the skeleton now declares the same `v0.35.0` floor this kit
needs.

## What it removes

A file an earlier release published and this one does not is in `retired` in
`publish.go`, with the SHA-256 of every version a release wrote — the content
with the project's module path written as `example.test/project`, so a digest
is the hash of that release's golden file. `retire` runs after the files are
written: under `--force` it removes a file whose bytes are one of those
versions, and the directory it leaves empty, and names the blank import that
goes with that directory; without `--force` it names the file as stale; under
`--dry-run --force` it says what would go. A file that differs from every
version — edited, or never the kit's — is left and said so. Today the list is
`resources/views/partials/login_form.kyse.go`, published from v0.8.0 to
v0.19.0. `TestEveryRetiredDigestIsAVersionThisKitPublished` ties each digest to
a fixture under `testdata/retired/`, taken from the golden file at the tag.

## What this repository holds

| | measured with |
| --- | --- |
| 5 Go source files, one package, no subdirectories | `ls *.go \| grep -v _test.go \| wc -l` |
| 29 template constants, one per file the command writes | `grep -ho 'const [a-zA-Z]*Template' views*.go \| wc -l` |
| 29 files published by `auth` — 17 views, 11 plain Go and 1 script | `go build -o /tmp/ui . && (cd ../arandu && /tmp/ui auth --dry-run \| wc -l)` |
| 22 of those refreshed by `auth --views` — 17 views plus `page.go`, `render.go`, `HomeController.go` and the two under `resources/js/` | `(cd ../arandu && /tmp/ui auth --views --dry-run \| wc -l)` |
| 29 golden files, byte for byte what is published | `find testdata -name '*.golden' \| wc -l` |
| 106 tests in 5 internal test files | `grep -h '^func Test' *_test.go \| wc -l` and `find . -maxdepth 1 -name '*_test.go' \| wc -l` |
| 1 file retired, removed by `--force` while it is still the kit's bytes, and 3 fixtures, one per version published | `sed -n '/^var retired/,/^}/p' publish.go \| grep -c 'Path:'` and `ls testdata/retired \| wc -l` |
| 23 routes mounted by the module it publishes, 9 for two-factor authentication, 10 of them controller actions | `grep -hE '^\tg\.(Get\|Post\|Action)\(' views_controllers.go views_auth_flow.go \| wc -l` and `grep -c '^\tg\.Action(' views_controllers.go` |
| 1 dependency, the publishing engine, and that is a CI step | `awk '/^require/,0' go.mod \| grep -c 'github.com'` |
| 5 files replaced without `--force`, the layout unit | `sed -n '/^var replaced/,/^}/p' publish.go \| grep -c 'true,'` |

Of the 17 views, 13 are screens — the layout, home, welcome, six base auth
screens and four two-factor screens — 4 are message bodies, an HTML part and a
plain-text part for each of the two messages the flow sends, and none is a
fragment. The three are counted separately by
`TestTheAuthViewsAreSeventeenAndWellFormed` in
`views_internal_test.go:21`, because they are different things: a mail body has
no layout, no navigation and no token, and a fragment has no layout either but
for the opposite reason — it is swapped **into** a page that already drew one.

**A form is bound, and a rejected one goes back to the form, through the
router.** The ten routes that read a form are registered with `Action`. Each
handler converts its form with `ctx.Bind` into a request struct declared beside
it — form tags, and a `LogValue` that says which fields arrived and nothing they
carried — reads no field by hand and calls no `Validate()`; the checks the form
needs stay in the handler. `Bind` trims every value but a password, which
reaches the handler as typed from hesape v0.50.2 on. A password stored trimmed
before that — chosen with spaces at its ends through the v0.21.0 handlers — gets
one more try: when the typed form is refused, is not a lock and differs from its
trimmed form, `verifyTrimmedPassword` in `LoginController_handlers.go` compares
the trimmed form, stores the password as typed through `Users.ResetPassword` on
success, and refuses the rest with the usual message, as one attempt and inside
the same timebox whether the account exists or not. Its doc comment says when to
delete it. A
handler that refuses the form returns `validation.Errors`, and a new password is
checked against `passwordPolicy` in `RegisterController.go`, the same
declaration the sign-up and reset screens hand the password box as
`AuthPage.PasswordPolicy`, so the checklist and the refusal are one rule. The
router answers: a 303 back to the
Referer for a page, with the messages and what was typed (minus every secret) in
the flash; `HX-Redirect` for htmx; a 422 problem document for a client that asked
for JSON. No published handler writes a refusal status, and `Module.screen`
draws every screen at 200 and puts what the flash carried on the page — the
messages in `view.Page.Errors`, the typed input in `view.Page.Old`, a notice as
the status line — so the components ask the page by field name and no handler
copies a message anywhere. A screen drawn after a form succeeded is reached by a
redirect too, through `Module.notify`, so the form on it has an address a
refusal can be sent back to and a reload never repeats the post. The layout
leaves htmx's response handling at its default: teaching it to swap a 422 would
be a second way to answer the same rejection.

**One flash and one Secure decision.** The kernel's router carries the flash the
application reads back, and `Routes` writes the module's notices with it; only a
router with none — one a test builds — is handed the flash `New` made. The
router is asked through a small interface rather than `r.Flash()`, so the
published file still compiles against a framework older than v0.53.0. The
printed wiring passes `cfg.Framework.Session.Secure` as the module's `secure`,
the value the session cookie and the kernel's flash follow, and the pending
two-factor cookie takes it. `TestTheWiringPassesTheOneSecureDecision` holds the
printed argument and `TestThePendingCookieCarriesTheSecureNewWasGiven` reads the
cookie off a running module. The compile and run gates pin framework v0.53.0 and
hesape v0.50.3, the versions skeleton v0.32.0 requires.

| gate | what it refuses |
| --- | --- |
| `TestARejectedFormIsReturnedToTheRouter` | a published Go file that writes a refusal status or draws a refusal itself |
| `TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy` | a route whose name, method or `Action` registration differs from the table — the last column is who answers a rejection |
| `TestARejectedSignInGoesBackToTheFormInEveryTransport` | runs the published module: page, htmx and JSON answers to a refused sign-in, and the reset code reached by a redirect |
| `TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn` | a field a handler rejects with that no screen has an input for, or a notice no screen draws |
| `TestTheLayoutLeavesHtmxResponseHandlingAtItsDefault` | an `htmx-config` in the published layout that sets anything but `includeIndicatorStyles:false` |
| `TestTheKitWritesItsNoticesWithTheRoutersFlash` | runs the published module: a notice or a refusal written with a flash other than the router's, or a router with no flash left without the module's |
| `TestAPasswordStoredTrimmedSignsInOnceAndIsStoredAsTyped` | runs the published sign-in against a user service that hashes for real: a password stored trimmed that stays locked out or is not rewritten as typed, a second attempt counted, a second form tried on a lock or on an empty trimmed form, or a refusal that differs or escapes the timebox |
| `TestNoPublishedHandlerReadsItsFormByHand` | a published controller that reads a field by hand or calls `Validate()`, or an `Action` whose handler does not call `ctx.Bind(&in)` |
| `TestTheChecklistAndTheHandlerAreOnePasswordPolicy` | a password box that chooses a password without `Policy: .PasswordPolicy`, a screen whose handler does not fill it, or a handler that checks a new password against anything but `passwordPolicy` |
| `TestTheQRCodeIsAnImageAndNoPublishedGoImportsHTMLTemplate` | published Go naming `html/template` or `template.HTML`, a `{!! !!}` that is not a component, or a setup screen not drawing the QR code as an image |
| `TestAScreenThatCallsWhatPageNoLongerDeclaresIsNotKeptBesideIt` | a run that keeps a file calling what `page.go` dropped |
| `TestForceRemovesOnlyWhatThisKitPublishedAndNoLongerDoes` | `--force` keeping a retired file the kit wrote, or removing an edited one or one it never wrote |

**The directory says which, and the source has to agree.** `layouts/` yields
sections, `partials/` and `mail/` carry no layout, everything else under
`resources/views/` extends one, and
`TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne` in
`publish_internal_test.go` reads the published bytes to hold each file to the
kind its path claims. It also refuses a narrowed swap anywhere but in a
fragment: an element carrying one asks the server for its own markup back, and a
screen answering that hands htmx a whole document for a form-shaped hole — the
header, the navigation and a second toaster land inside the card, with a green
build and a correct status. The kit shipped exactly that on `auth/login.kyse.go`,
then moved the form to a partial of its own answered with a 422, and finally
gave the partial up: a refused sign-in is a redirect, the screen is drawn whole,
and `TestTheKitPublishesNoFragmentAndAsksForNone` keeps the kit free of
fragments.

A narrowed swap has **two spellings**, and `narrowedSwap` in that file carries
both. `hx-target=`/`hx-swap=` is the attribute a view writes; `HxTarget`/`HxSwap`
is a prop of a kyse component, and
`components.Button(components.ButtonProps{HxTarget: "#form"})` renders the
attribute into the served document while putting neither literal in the source.
The gate read the attribute alone and passed that screen — silently, since
nothing here renders a page. The props are read off the component library and
are a closed set: `ButtonProps.HxTarget`, `ButtonProps.HxSwap` and
`MenuItem.HxTarget` are every field there that renders one of the two.
`HxPost`, `HxGet` and `HxConfirm` are not among them — they say where to ask,
not what comes back.

**Who owns which state, and what will fail if you get it wrong.** Four things in
a published page can hold a value, and what tells them apart is when each is next
drawn: a component is re-run wherever its caller is and keeps nothing, the layout
runs once per document and no swap redraws it, a screen is the whole document for
one request, and a fragment is what is inside one swap target. The kit publishes
no fragment, so the seam with no compiler behind it — the screen and the piece
of it answered alone, one type through `@include` — is not one it has. The two
gates that read it, `TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap` and
`TestNothingTheLayoutDrawsIsRedrawnInsideASwap`, left with the fragment and are
in the history for the day one comes back. The layout renders through
`view.Layout` and a component is handed the page as `components.Page`, so
neither can name a field of a screen; these read the published bytes for the
rest:

| gate | what it refuses |
| --- | --- |
| `TestTheKitPublishesNoFragmentAndAsksForNone` | a view under `partials/`, or published Go that calls `.Fragment(` — every screen here is answered whole |
| `TestNoPublishedViewKeepsStateInTheBrowser` | `x-data` and its relatives in any published view: nothing the layout loads reads one, and `script-src 'self'` has no `unsafe-eval` |
| `TestTheKitsLayoutKeepsWhatTheSkeletonsLayoutCarries` | a head element the skeleton's layout has and this one does not — publishing replaces that file with no flag, so a project loses it silently |
| `TestEveryAssetAPublishedViewAsksForIsOneSomethingRegisters` | a `view.Asset("…")` in a published view naming an asset neither the runtime embeds nor this kit delivers — `view.Asset` panics on an unregistered name, so that is every request of every project answered with a panic |
| `TestEveryFileTheKitEmbedsIsOnePublishedBesideItAndNotEmpty` | a `//go:embed` naming a file the kit does not publish, or publishes empty — the first does not build and the second panics at start-up, because `RegisterAsset` refuses a zero-byte body |

The two asset gates are the ones the layout earned. It gained a `<script>` for
`custom.js` while the only package registering that name was one the skeleton
had begun carrying the same day, and `resources/views/layouts/app.kyse.go` is in
`replaced` — so publishing into any older project wrote a layout that could not
render, with nobody having chosen it. The kit now publishes `resources/js/js.go`
and `resources/js/custom.js` alongside the layout, and neither is in `replaced`:
a project that already has them keeps its own script.

What the browser does own is what dies with the tab, and it has a home: `ui.js`,
loaded by the layout. It binds on `document`, dispatches on `data-` attributes,
keeps open and selected in the ARIA the markup already carries, and evaluates
nothing — so the DOM is the only copy and swapped-in markup is live where it
lands.

## What does not exist here

Reaching for one of these is the most common way to waste an afternoon. None of
them is missing by accident; each was considered and refused.

| A model reaches for | What is here instead |
| --- | --- |
| a `resources/views/` directory to edit | a Go string constant. `authLoginViewTemplate` in `views.go` **is** `resources/views/auth/login.kyse.go` |
| a second dependency — a template library, a CLI framework, a diff library | nothing. The one require is `github.com/arandu-io/hesape`, for `publish.Merge`; a CI step fails on any other, and every require here is a download for everyone who runs the command |
| importing the CLI's renderer | a 40-line copy in `publish.go`. Importing it would make this module depend on the CLI, and the point of publishing from here is that the CLI is not in the way |
| writing a merge that keeps custom blocks | `publish.Merge` from `hesape/publish`. There were two implementations of that algorithm and they answered the same question, so there is one |
| a preset argument — `auth bootstrap`, `auth tailwind` | one set of screens. `auth` with an extra word is refused rather than ignored, because a flag typed after an ignored word is switched off silently |
| a generator that edits `bootstrap/app.go` | text printed to the terminal for a person to paste. One line somebody reads beats a file edited behind their back |
| `os.WriteFile` over an existing file | `write` in `publish.go`, which reads the file first and hands it to `publish.Merge` to carry the custom blocks over. It used to stat and overwrite, and it ate people's work |
| a `_test.go` beside the code with no `_internal` suffix | `tests/test-layout-guard.sh`, which fails the build. This is a `package main`, so its tests are internal and the suffix says so |

## The two rules everything else follows from

**Every published file is the project's, and a republish must prove it.** The
command is meant to be run again for a fix from a newer version. That only works
because what somebody wrote inside `arandu:begin custom` … `arandu:end custom`
is carried forward, and because five files — the layout unit — are the only ones
replaced without a flag. A full republish therefore writes 5 and keeps 24. Nine
of the 29 published files carry a custom block: 5 in Go comment syntax and 4 in
kyse comment syntax, because a `//` below the package clause of a `.kyse.go` is
markup that would be printed into an e-mail.

**The golden files are the product.** They are not a convenience for the suite;
they are the 29 files a project receives. `TestAuthGolden` in
`publish_internal_test.go:53` compares them byte for byte, and CI regenerates
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
`flow_internal_test.go:634` reads every published file to keep it that way.

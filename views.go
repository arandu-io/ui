package main

import (
	"path/filepath"
	"strings"
)

// The thirteen screens of the starter kit, and the one fragment they are answered
// with.
//
// The screens are the ones every application has, at the paths people look for:
// the layout, the dashboard, the welcome page, sign in, sign up, email
// verification, and the three password screens. Keeping the names and the tree
// is the whole point -- somebody looking for the password reset form opens
// `resources/views/auth/passwords/reset.kyse.go` and finds it there.
//
// Underneath, nothing is borrowed. The styling is Basecoat's component classes,
// the assets are the ones the binary already serves embedded, and the helpers a
// template usually reaches for -- config, route, the signed-in user -- are
// fields of one typed struct. A view here cannot reach for request state on its
// own, so a link that drifts is a compile error rather than a dead anchor.
//
// # Where the struct lives
//
// In the controller package, not in the views. `AuthPage` is declared in
// app/Http/Controllers/Auth, which is where its fields are filled in, and each
// screen names it in one line:
//
//	@go
//	type LoginData = authui.AuthPage
//	@endgo
//
// One shape, one declaration. The alternative -- the layout declaring it and
// the pages inheriting -- meant the type of every screen changed when the layout
// was replaced, and a page and its controller are one unit that has to move
// together.
//
// Each screen gets its own name for it because a directory of views is one Go
// package, and three files in `auth/` cannot all declare `AuthPage`.
//
// # What is deliberately absent
//
// No `@auth`, no `@error`, no `@can`, no `<x-component>`: kyse's directive set
// is closed. The guest branch of the navigation bar is `@if(!.SignedIn())`, and
// a validation message is a field of the Field component. That is more
// characters and one less language.

// AuthViews returns the views plus the struct they render, ready to be written
// into a project.
//
// The sources keep the conventional tree under `resources/views`, and
// `aru view:build` writes the generated Go beside each one -- so `auth/` holds
// the four files of `auth/` and nothing else. Each directory is its own package,
// which costs one blank import per directory in bootstrap and is the same
// registration the whole design is built on.
//
// # Three kinds of file, told apart by where they are
//
// The directory is the declaration, and nothing else has to be remembered:
//
//   - layouts/ is the frame. It yields sections and extends nothing.
//   - partials/ is a fragment: no layout, so it can be swapped into a page that
//     is already on screen.
//   - mail/ is a message body: no layout either, for a different reason -- there
//     is no navigation and no token in an e-mail.
//   - everything else is a screen, and extends the layout.
//
// TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne reads the published
// bytes and holds each file to the kind its path claims.
//
// # Who owns which state
//
// Four things here can hold a value, and what separates them is not what each
// is allowed to know. It is when each is next drawn:
//
//   - A component holds nothing. It is a function from props to markup, run
//     wherever the markup that calls it is run, and it reads the page through
//     an interface -- so it cannot name a field, and there is nothing for it to
//     carry from one call to the next.
//   - The layout holds the chrome, and only what cannot change without a full
//     load. It is drawn once per document and no swap redraws it, so whatever
//     it says stays what it said when the document arrived.
//   - A screen holds the state of the whole document for one request. The part
//     of it outside a swap target is frozen at the first swap, exactly like the
//     layout's chrome.
//   - A fragment holds what is inside its own swap target and nothing else.
//     What it draws is replaced whole by the next answer.
//
// The rule at the boundary follows from the last two: a handler answering with
// a fragment may fill only what that fragment draws. A field that only the
// screen around it draws is computed, sent and thrown away, because the screen
// is not being redrawn -- and nothing fails, which is what makes it worth a
// test rather than a sentence.
//
// Two of those three seams have a compiler behind them already. A layout
// renders through view.Layout, an interface, so naming a screen's field there
// does not build; a component is handed the page as components.Page, another
// interface, so it cannot name one either. The third has none: @include passes
// the page's own data straight through, and the fragment names that same
// struct, so page state and fragment state are one type and nothing keeps them
// apart. TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap and
// TestNothingTheLayoutDrawsIsRedrawnInsideASwap read the published bytes where
// the compiler cannot.
//
// None of the four is the browser, and the browser has a share: what dies with
// the tab and the server never needs to hear about. That is ui.js's, which the
// layout loads -- it dispatches on data- attributes and evaluates nothing, so
// the DOM holds the only copy. No view here reaches for anything else, and
// TestNoPublishedViewKeepsStateInTheBrowser is what keeps it that way.
func AuthViews(m Module) ([]File, error) {
	dir := filepath.Join("resources", "views")

	sources := []struct {
		path string
		tmpl string
	}{
		{filepath.Join("app", "Http", "Controllers", "Auth", "page.go"), authPageTemplate},
		{filepath.Join(dir, "layouts", "app.kyse.go"), authLayoutViewTemplate},
		{filepath.Join(dir, "home.kyse.go"), authHomeViewTemplate},
		{filepath.Join(dir, "welcome.kyse.go"), authWelcomeViewTemplate},
		{filepath.Join(dir, "auth", "login.kyse.go"), authLoginViewTemplate},
		{filepath.Join(dir, "auth", "register.kyse.go"), authRegisterViewTemplate},
		{filepath.Join(dir, "auth", "verify.kyse.go"), authVerifyViewTemplate},
		{filepath.Join(dir, "auth", "passwords", "confirm.kyse.go"), authPasswordConfirmViewTemplate},
		{filepath.Join(dir, "auth", "passwords", "email.kyse.go"), authPasswordEmailViewTemplate},
		{filepath.Join(dir, "auth", "passwords", "reset.kyse.go"), authPasswordResetViewTemplate},
		{filepath.Join(dir, "auth", "two-factor", "challenge.kyse.go"), authTwoFactorChallengeViewTemplate},
		{filepath.Join(dir, "auth", "two-factor", "recovery.kyse.go"), authTwoFactorRecoveryViewTemplate},
		{filepath.Join(dir, "auth", "two-factor", "setup.kyse.go"), authTwoFactorSetupViewTemplate},
		{filepath.Join(dir, "auth", "two-factor", "recovery-codes.kyse.go"), authRecoveryCodesViewTemplate},

		// The one fragment. It is the sign-in form, because the sign-in form is
		// the one control in the kit that asks to be answered on its own -- see
		// authLoginFormPartialTemplate for why that makes it a file with no
		// layout rather than a section of the screen it is drawn on.
		{filepath.Join(dir, "partials", "login_form.kyse.go"), authLoginFormPartialTemplate},

		// The message bodies. Both parts of both messages: a mail with no
		// plain-text part is filed as spam more often, and shows nothing at all
		// in a client that cannot render HTML.
		{filepath.Join(dir, "mail", "verify-email.kyse.go"), verifyMailViewTemplate},
		{filepath.Join(dir, "mail", "verify-email-text.kyse.go"), verifyMailTextTemplate},
		{filepath.Join(dir, "mail", "password-reset.kyse.go"), passwordMailViewTemplate},
		{filepath.Join(dir, "mail", "password-reset-text.kyse.go"), passwordMailTextTemplate},
	}

	var out []File
	for _, s := range sources {
		content, err := render(filepath.Base(s.path), s.tmpl, m)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: s.path, Content: content})
	}
	return out, nil
}

// authPageTemplate is the struct the thirteen screens render from.
//
// It is Go, not kyse, and it lives with the controller rather than with the
// views. That is where the fields are set, so a field that is added and never
// filled in is visible in one file, and a screen that reads one that does not
// exist is a compile error at the line of the `.kyse.go` that read it.
//
// It embeds view.Page, which is what makes it fit the layout: the title, the
// description, the token and the navigation come from there, declared once in
// the framework instead of once per project.
//
// It also keeps the CSRF token and all two-factor provisioning material out of
// serialized diagnostics.
const authPageTemplate = `package authui

import (
	"encoding/json"
	"html/template"
	"log/slog"

	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/kyse/components"
)

// AuthPage is what every screen of the starter kit renders from.
//
// One struct for the thirteen screens rather than one per page: they share a layout
// and a shape, and a field a given screen does not use stays at its zero value
// and is never read. That is cheaper than thirteen structs repeating the same form
// state, and it is why each view names this one in a single line.
//
// The chrome is not repeated here at all: the embedded view.Page carries the
// title, the description, the canonical URL, the token and the navigation, and
// is what makes this struct satisfy view.Layout.
//
// Nothing here is a helper a view reaches for on its own. There is no route(),
// no config() and no auth(): a URL, the application name and the signed-in
// person are fields the handler filled in, so a name that drifts is a compile
// error instead of a blank link -- and a form can never carry another session's
// token under load.
//
// # Which side of a swap a field is on
//
// The form under resources/views/partials/ renders from this same struct:
// @include hands the page's data through unchanged, so both the screen and the
// one part of it that is answered alone read these fields. Nothing in the type
// separates them, and the two are not refreshed together -- what the form draws
// is replaced when the form is swapped, and what only the screen around it
// draws is not redrawn at all.
//
// It decides what a handler may fill. Answering the form alone with a field
// only the screen draws sends the value inside a response whose other half the
// browser discards: the right status, the right markup in the hole, and a
// sentence nobody ever reads. Fill what the form draws, or answer the screen.
type AuthPage struct {
	view.Page

	// HasPasswordReset moves the "is this route registered" question to the
	// data: an application that did not register that route hides the link
	// rather than linking to a 404.
	HasPasswordReset bool

	// The addresses these screens post to and link to, beyond the navigation
	// view.Page already carries. They come from the router, through the handler.
	DashboardURL          string
	PasswordRequestURL    string
	PasswordEmailURL      string
	PasswordUpdateURL     string
	PasswordConfirmURL    string
	VerificationConfirmURL string
	VerificationResendURL string
	TwoFactorChallengeURL string
	TwoFactorRecoveryURL string
	TwoFactorSetupURL string
	TwoFactorSetupConfirmURL string
	TwoFactorDisableURL string
	RecoveryCodesURL string

	// Status is the one-shot message a redirect left behind, such as the
	// confirmation that a reset code was sent. Empty means nothing to say.
	Status string
	// Resent says a fresh verification code just went out.
	Resent bool

	// Name, Email and Remember are what the person typed on the attempt that
	// was rejected. The password is deliberately absent: it is never sent back.
	Name     string
	Email    string
	Remember bool

	// Provisioning material is rendered once and deliberately omitted from
	// MarshalJSON and LogValue.
	QRCodeSVG        string
	SecretKey        string
	RecoveryCodesText string

	// The validation messages, one field at a time. Empty means the field was
	// accepted -- there is no @error directive to ask a bag on the side.
	NameError                 string
	EmailError                string
	EmailCodeError            string
	AuthenticatorCodeError    string
	RecoveryCodeError         string
	PasswordError             string
	PasswordConfirmationError string
}

// Compile-time proof that these screens fit the layout, and that a component
// can ask them about a field.
var (
	_ view.Layout     = AuthPage{}
	_ components.Page = AuthPage{}
)

// FieldError answers the question a kyse component asks about an input.
//
// components.FieldProps carries the page and the field name, and asks; it does
// not carry the message as a third prop, because that meant writing the field
// name twice in one call and the two could disagree without anything saying so.
// See the doc comment on components.FieldProps for the whole of that argument.
//
// These screens keep the messages in named fields rather than in the map
// view.Page carries, because a typed field is one the compiler checks: a
// handler that sets PasswordConfirmatonError does not build, where a map key
// spelt the same way is simply never read. This method is the seam between the
// two -- the fields stay the source, and the components see one interface.
//
// A name with no field of its own falls through to view.Page, which is where a
// message put in the map by anything generic would be.
func (p AuthPage) FieldError(name string) string {
	switch name {
	case "name":
		return p.NameError
	case "email":
		return p.EmailError
	case "email_code":
		return p.EmailCodeError
	case "authenticator_code":
		return p.AuthenticatorCodeError
	case "recovery_code":
		return p.RecoveryCodeError
	case "password":
		return p.PasswordError
	case "password_confirmation":
		return p.PasswordConfirmationError
	}
	return p.Page.First(name)
}

// TrustedQRCode marks the SVG produced by hesape/qr as trusted markup. The
// two-factor handler reaches this boundary only after qr.Encode and Code.SVG
// produce the validated document; arbitrary strings must remain escaped.
func TrustedQRCode(svg string) template.HTML { return template.HTML(svg) }

// redacted is what a secret looks like once it has left this package.
//
// The value never appears. Whether there was one does: an empty string stays
// empty, and anything else becomes the marker. A secret that simply vanished
// from the output would read exactly like a field nobody filled in, and "the
// form posted an empty token" is the failure these pages are dumped to find.
func redacted(secret string) string {
	if secret == "" {
		return ""
	}
	return "[redacted]"
}

// MarshalJSON keeps the CSRF token and two-factor provisioning material out of
// anything that serializes the page.
//
// It names the fields that may leave, rather than the ones that may not. A
// field added to the struct later does not appear until it is named here, which
// is the direction that cannot leak by accident -- the reverse spelling grows a
// hole every time somebody adds a field and does not think about this method.
//
// The CSRF token carries a marker; provisioning material is omitted entirely.
func (p AuthPage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Title         string
		AppName       string
		Path          string
		Authenticated bool
		UserName      string
		Token         string

		HasPasswordReset      bool
		DashboardURL          string
		PasswordRequestURL    string
		PasswordEmailURL      string
		PasswordUpdateURL     string
		PasswordConfirmURL    string
		VerificationConfirmURL string
		VerificationResendURL string
		TwoFactorChallengeURL string
		TwoFactorRecoveryURL string
		TwoFactorSetupURL string
		TwoFactorSetupConfirmURL string
		TwoFactorDisableURL string
		RecoveryCodesURL string

		Status   string
		Resent   bool
		Name     string
		Email    string
		Remember bool

		NameError                 string
		EmailError                string
		EmailCodeError            string
		AuthenticatorCodeError    string
		RecoveryCodeError         string
		PasswordError             string
		PasswordConfirmationError string
	}{
		Title:         p.Page.Title,
		AppName:       p.Page.AppName,
		Path:          p.Page.Path,
		Authenticated: p.Page.Authenticated,
		UserName:      p.Page.UserName,
		Token:         redacted(p.Page.Token),

		HasPasswordReset:      p.HasPasswordReset,
		DashboardURL:          p.DashboardURL,
		PasswordRequestURL:    p.PasswordRequestURL,
		PasswordEmailURL:      p.PasswordEmailURL,
		PasswordUpdateURL:     p.PasswordUpdateURL,
		PasswordConfirmURL:    p.PasswordConfirmURL,
		VerificationConfirmURL: p.VerificationConfirmURL,
		VerificationResendURL: p.VerificationResendURL,
		TwoFactorChallengeURL: p.TwoFactorChallengeURL,
		TwoFactorRecoveryURL: p.TwoFactorRecoveryURL,
		TwoFactorSetupURL: p.TwoFactorSetupURL,
		TwoFactorSetupConfirmURL: p.TwoFactorSetupConfirmURL,
		TwoFactorDisableURL: p.TwoFactorDisableURL,
		RecoveryCodesURL: p.RecoveryCodesURL,

		Status:   p.Status,
		Resent:   p.Resent,
		Name:     p.Name,
		Email:    p.Email,
		Remember: p.Remember,

		NameError:                 p.NameError,
		EmailError:                p.EmailError,
		EmailCodeError:            p.EmailCodeError,
		AuthenticatorCodeError:    p.AuthenticatorCodeError,
		RecoveryCodeError:         p.RecoveryCodeError,
		PasswordError:             p.PasswordError,
		PasswordConfirmationError: p.PasswordConfirmationError,
	})
}

// LogValue implements slog.LogValuer, so a log line handed the whole page
// records which screen it was and nothing else.
//
// Shorter than MarshalJSON on purpose. A log line is shipped to an aggregator
// and kept, and the address somebody typed into a sign-in form is not something
// to keep there; the debug page is one request, on one laptop, in development.
func (p AuthPage) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("path", p.Page.Path),
		slog.String("title", p.Page.Title),
		slog.Bool("authenticated", p.Page.Authenticated),
	)
}
`

// authLayoutViewTemplate is the application layout.
//
// It declares nothing. The contract it renders with is view.Layout, published by
// the framework, and the chrome is view.Page -- so replacing this file replaces
// a frame and not a type, and the pages already in the project keep compiling.
// That is the whole reason the kit can be installed in a project that has pages.
//
// Four things here are load-bearing and easy to delete by accident:
//
//   - hx-headers on <body>: without it every hx-post fails the CSRF check, and
//     the failure reads like a broken session rather than a missing attribute;
//   - hx-boost on <body>: without it every link is a full page load;
//   - the asset list, which is content-addressed and same-origin, because the
//     CSP is script-src 'self' and a CDN would mean loosening it;
//   - the icon. This file replaces the skeleton's layout outright, so an element
//     only the skeleton had is one the project loses without a word -- and a
//     favicon silently back to the browser default is not something anybody
//     notices, since /favicon.ico keeps answering 200 either way.
//
// The head here is checked against the skeleton's, element by element, in
// TestTheKitsLayoutKeepsWhatTheSkeletonsLayoutCarries.
//
// # What it may hold
//
// The chrome, and nothing that changes without a full load. This file runs once
// per document: a swap replaces markup inside the page and never re-runs the
// layout, so every value here is the one the server gave when the document was
// fetched, for as long as the tab stays open.
//
// The token in hx-headers is the case worth reading, because it looks like a
// counter-example and is not. It is frozen with the rest, and it keeps working
// because the issuer signs a nonce and an expiry against the session id rather
// than recording one token per session -- any token issued for that session
// validates until it expires. A token bound to something that changed, such as
// the id a sign-in rotates, would be stale here with nothing to say so; that
// path answers a redirect, and a redirect fetches this file again.
//
// TestNothingTheLayoutDrawsIsRedrawnInsideASwap keeps the other half: a value
// drawn here and again inside a swap target is one value with two copies, and
// only one of them is ever refreshed.
const authLayoutViewTemplate = `//go:build kyse

package layouts

import "github.com/arandu-io/kyse/components"

<!doctype html>
<html lang="en" class="h-full">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">

	{{-- What a rejected form means.
	     htmx swaps a response only when its table of response handling says to,
	     and the default in the copy this framework embeds ends with
	     {"code":"[45]..","swap":false} -- so a 422 is fetched, is correct, and is
	     thrown away. The person sees the form they submitted, unchanged, with no
	     message on it, and concludes the button does nothing. Every screen this
	     kit publishes is a form, so without this line none of them can say why
	     they refused anything.
	     422 comes before the catch-all because htmx takes the first entry that
	     matches. See framework/http/context.go.

	     includeIndicatorStyles is false because htmx would otherwise inject a
	     <style> element of its own on the first request, and the policy is
	     style-src 'self' with no unsafe-inline: the browser refuses it, once per
	     page, in a console nobody has open. The classes it would have written are
	     in the stylesheet already. --}}
	<meta name="htmx-config" content='{"includeIndicatorStyles":false,"responseHandling":[{"code":"204","swap":false},{"code":"422","swap":true},{"code":"[23]..","swap":true},{"code":"[45]..","swap":false,"error":true}]}'>
	<title>{{ .PageTitle() }}</title>
	{{-- Both icons, because a browser picks by what it is given: the .ico is the
	     one asked for by address and sizes="any" is what stops a modern browser
	     rasterising it at one size, and the .png is what everything else takes. --}}
	<link rel="icon" href="/favicon.ico" sizes="any">
	<link rel="icon" href="/favicon.png" type="image/png">

	{{-- What a page says about itself, written only when the page filled it in.
	     An empty description is worse than none: a search engine that finds one
	     stops looking for a better sentence in the body. --}}
	@if(.PageDescription() != "")
		<meta name="description" content="{{ .PageDescription() }}">
		<meta property="og:description" content="{{ .PageDescription() }}">
	@endif
	@if(.CanonicalURL() != "")
		<link rel="canonical" href="{{ .CanonicalURL() }}">
		<meta property="og:url" content="{{ .CanonicalURL() }}">
	@endif
	<meta property="og:title" content="{{ .PageTitle() }}">
	<meta property="og:site_name" content="{{ .BrandName() }}">
	<meta property="og:type" content="website">
	<meta name="twitter:card" content="summary_large_image">

	<link rel="stylesheet" href="{{ view.URL("app.css") }}">
	<script src="{{ view.URL("htmx.min.js") }}" defer></script>
	{{-- These four, and no client framework beside them. State on this stack
	     lives on the server: a handler decides and answers markup that is already
	     correct, so there is no second copy of it here to keep in step. The hx-
	     attributes below say only where to ask and what to replace, and nothing
	     reads a value back out of one.

	     What is left is what dies with the tab and the server never needs to hear
	     about -- a menu that is open, a row that is selected -- and ui.js is where
	     it lives. It binds once on document and dispatches on data- attributes,
	     so markup htmx swaps in is live the moment it lands and there is nothing
	     to initialise; open and selected are kept in the ARIA the markup already
	     carries, which makes the DOM the only copy. It reads no expression, and
	     it could not: the policy is script-src 'self' with no unsafe-eval, so
	     anything compiling a directive out of a string throws before it runs.

	     A fifth tag would also have to have something to serve, and view.URL is
	     what decides: it panics on a name nothing registered, naming the ones
	     that are, so a tag for an asset that does not exist takes the request
	     down rather than 404ing quietly on every page. Register it with
	     view.RegisterAsset before writing the tag. --}}
	<script src="{{ view.URL("ui.js") }}" defer></script>
	<script src="{{ view.URL("basecoat.bundle.js") }}" defer></script>
	{{-- This project's own behaviours, carried over because publishing replaces
	     this layout rather than adding to it: a page that lost the tag would
	     leave every behaviour the project registers silently unreachable, and
	     the symptom -- a mask that works everywhere but on the auth pages -- is
	     one nobody attributes to a layout they did not write.

	     After ui.js, because it registers into the registry ui.js creates:
	     deferred scripts run in document order. --}}
	<script src="{{ view.URL("custom.js") }}" defer></script>
	<script src="{{ view.URL("theme.js") }}"></script>
</head>
<body hx-boost="true" hx-headers='{"X-CSRF-Token": "{{ .CSRFToken() }}"}' class="bg-background text-foreground min-h-full antialiased">
	<div class="flex min-h-full flex-col">
		<header class="border-b">
			<nav class="mx-auto flex h-16 max-w-5xl items-center justify-between gap-4 px-6">
				<a href="{{ .HomeLink() }}" class="text-sm font-semibold tracking-tight">{{ .BrandName() }}</a>
				<div class="flex items-center gap-2 text-sm">
					{!! components.ThemeToggle(components.ThemeToggleProps{}) !!}
					@if(!.SignedIn())
						<a href="{{ .LoginLink() }}" class="btn" data-variant="ghost" data-size="sm">Login</a>
						@if(.RegisterLink() != "")
							<a href="{{ .RegisterLink() }}" class="btn" data-size="sm">Register</a>
						@endif
					@endif
					@if(.SignedIn())
						<span class="text-muted-foreground">{{ .SignedInName() }}</span>
						<form method="post" action="{{ .LogoutLink() }}">
							@csrf
							<button type="submit" class="btn" data-variant="ghost" data-size="sm">Logout</button>
						</form>
					@endif
				</div>
			</nav>
		</header>

		{{-- The boundary. Everything outside this element is drawn once, when the
		     document is fetched: a swap replaces markup inside the page and never
		     re-runs this file, so the header, the token above and the tray below
		     keep saying what they said then, for as long as the tab is open.

		     A value that changes on an interaction belongs inside the target that
		     changes it, and not here. Drawn in both places it is one value with
		     two copies, and only the inner one is ever refreshed. --}}
		<main class="mx-auto w-full max-w-5xl grow px-6 py-10">
			@yield('content')
		</main>

		{{-- The tray flash messages land in. An endpoint that saves something
		     answers with a toast fragment and hx-swap="beforeend" on this
		     element; the vendored script arms whatever appears inside it. --}}
		<div id="toaster" class="toaster" aria-live="polite"></div>
	</div>
</body>
</html>
`

// authHomeViewTemplate is the dashboard, the screen you land on after signing in.
const authHomeViewTemplate = `//go:build kyse

package views

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// HomeData is what HomeController.Index hands this page.
type HomeData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-2xl">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Dashboard</h1>
			</header>
			<div class="px-6 py-6 text-sm">
				@if(.Status != "")
					<div class="mb-4">
						{!! components.Alert(components.AlertProps{Title: .Status}) !!}
					</div>
				@endif
				<p class="text-muted-foreground">You are logged in.</p>
			</div>
		</section>
	</div>
@endsection
`

// authWelcomeViewTemplate is the landing page.
//
// The usual landing page is a standalone document with its own <html> and an
// inlined stylesheet. This one extends the layout like every other page: a
// second page shell would be a second way to draw a page, and the shell is where
// the CSRF wiring lives.
const authWelcomeViewTemplate = `//go:build kyse

package views

import authui "<% .ModulePath %>/app/Http/Controllers/Auth"

@go
// WelcomeData is what the landing page draws.
type WelcomeData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto flex w-full max-w-2xl flex-col items-start gap-6 py-12">
		<h1 class="text-3xl font-semibold tracking-tight">{{ .AppName }}</h1>
		<p class="text-muted-foreground text-base">The routing, the controllers and the markup are on the server. There is no API layer in between and no router in the browser, and what travels on an interaction is a fragment of HTML.</p>
		<div class="flex flex-wrap items-center gap-3">
			@if(.Authenticated)
				<a href="{{ .DashboardURL }}" class="btn">Dashboard</a>
			@endif
			@if(!.Authenticated)
				<a href="{{ .LoginURL }}" class="btn">Login</a>
				@if(.RegisterURL != "")
					<a href="{{ .RegisterURL }}" class="btn" data-variant="outline">Register</a>
				@endif
			@endif
		</div>
	</div>
@endsection
`

// authLoginViewTemplate is the sign-in screen.
//
// It draws Status, and that block is not decoration. Two handlers render this
// screen to say something good happened -- the address was confirmed, the
// password was changed -- and with no block for it the string was computed,
// passed and thrown away: somebody who had just finished a password reset was
// shown an ordinary sign-in form with nothing on it to say the reset worked.
//
// The form itself is not here. It is a partial, because it is the one control
// the kit publishes that is answered on its own -- see
// authLoginFormPartialTemplate.
const authLoginViewTemplate = `//go:build kyse

package auth

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// LoginData is what the sign-in screen draws.
type LoginData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		{{-- The one-shot message a redirect or another handler left behind: an
		     address just confirmed, a password just changed. It is above the card
		     because it is about what already happened, not about what to type. --}}
		@if(.Status != "")
			<div class="mb-6">
				{!! components.Alert(components.AlertProps{Title: .Status}) !!}
			</div>
		@endif

		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Login</h1>
			</header>

			{{-- The form is a file of its own, and it is the one part of this
			     screen the server ever answers alone: a rejected sign-in comes
			     back as the form and nothing else, and htmx puts it where this
			     one is. @include hands over this page's data unchanged, so the
			     form reads the same struct whichever of the two drew it. --}}
			@include('partials.login_form')
		</section>
	</div>
@endsection
`

// authLoginFormPartialTemplate is the sign-in form, and the one fragment the kit
// publishes.
//
// It is a file of its own because of the two attributes on the form:
// hx-target="this" and hx-swap="outerHTML" say that a rejected sign-in replaces
// the form and nothing else. What the server answers has to be the shape of that
// hole. A view that extends the layout is a whole document, and htmx handed one
// for a form-shaped hole puts the header, the navigation and a second toaster
// inside the card -- with a green build, a correct status and a page that looks
// like it rendered twice.
//
// So the rule is mechanical rather than remembered: a view under partials/ has
// no layout, every other screen has one, and
// TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne reads the bytes of every
// published view to keep the two apart. It is the same shape the portal already
// uses for the marks it draws over and over.
//
// The screen draws it with @include, which hands over the page's own data
// unchanged, and the handler answers it directly with Module.fragment. AuthPage
// either way, so the form reads the same fields whichever one rendered it.
//
// That last sentence is also the hole. One struct for both sides means nothing
// in the type says which fields survive a swap of this file and which are drawn
// by the screen around it and therefore are not redrawn at all. A narrower
// struct here would not close it either: @include passes the page's own data
// through untouched, so the generated function would assert a type it was never
// given and fail at render rather than at build.
// TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap is what checks it
// instead, by reading which fields each handler fills against which fields this
// file draws.
const authLoginFormPartialTemplate = `//go:build kyse

// Package partials holds the parts of a screen the server answers on their own.
//
// A file here draws exactly its own markup and no layout around it, which is
// what lets it be swapped into the middle of a page that is already on screen.
// A file that draws a whole document belongs beside the screens instead.
//
// # Where state lives
//
// On the server, and the answer to a request is what says so. A handler reads
// the form, decides, and writes markup that is already correct -- so there is no
// second copy of the truth in the browser to keep in step, and nothing to
// reconcile when the two disagree.
//
// An hx- attribute holds no state. It says where to ask (hx-post), what to
// replace (hx-target) and how (hx-swap), and that is a routing decision about
// the DOM: nothing reads a value back out of one to decide anything.
//
// The browser owns what dies with the tab and the server never needs to hear
// about -- a disclosure that is open, a field that has focus. There is no client
// framework here to hold it: the policy is script-src 'self' with no
// unsafe-eval, so anything that compiles a directive from a string throws before
// it runs. What needs that shape is written with <details>, :focus-within or a
// checkbox; what does not fit is an endpoint.
//
// A form value is not client state. The address still in the box after a
// rejected sign-in came back from the server in the answer below.
//
// # What a file here may hold
//
// What is inside its own swap target, and nothing outside it. hx-target="this"
// with hx-swap="outerHTML" makes the target this form, so an answer replaces
// everything the file draws and reaches nothing else: the header, the tray and
// whatever the screen draws around the @include all keep what they already had.
//
// So a handler answering this file alone may fill only what this file draws. A
// field the screen draws and this one does not is computed, sent and dropped --
// the response carried it, the browser kept the part it asked for, and nothing
// failed. A value that has to be seen is drawn here, or the whole screen is
// what answers.
package partials

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// LoginFormData is what the sign-in form draws.
//
// It is the sign-in screen's own struct: @include hands the page's data
// straight through, and the handler that answers this form alone fills the same
// one.
type LoginFormData = authui.AuthPage
@endgo

{{-- One element, and deliberately: hx-swap="outerHTML" replaces the target with
     everything this file draws, so a second top-level element here would land
     beside the form rather than inside it.

     method="post" and action are the path with scripts off. htmx never reaches
     them, and without them a browser that is not running it has nothing to
     submit. --}}
<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .LoginURL }}"
	hx-post="{{ .LoginURL }}" hx-target="this" hx-swap="outerHTML">
	@csrf

	{!! components.Field(components.FieldProps{
		Name: "email", Label: "Email", Type: "email",
		Value: .Email, Page: .,
		Autocomplete: "username", Required: true, Autofocus: true,
	}) !!}

	{!! components.Field(components.FieldProps{
		Name: "password", Label: "Password", Type: "password",
		Page: .,
		Autocomplete: "current-password", Required: true,
	}) !!}

	<label class="flex items-center gap-2 text-sm">
		{{-- checked is a presence attribute: a browser reads the box as ticked
		     whether the value is "true", "false" or empty, so what is conditional
		     is the attribute and not its value. @if writes the whole attribute or
		     none of it, which is how every other boolean attribute in a kyse view
		     is drawn. --}}
		<input
			class="input"
			type="checkbox"
			name="remember"
			value="1"
			@if(.Remember)
				checked
			@endif
		>
		Remember me
	</label>

	<div class="flex items-center justify-between gap-3">
		<button type="submit" class="btn">Login</button>
		@if(.HasPasswordReset)
			<a class="text-muted-foreground text-sm hover:underline" href="{{ .PasswordRequestURL }}">Forgot your password?</a>
		@endif
	</div>
</form>
`

// authRegisterViewTemplate is the sign-up screen.
const authRegisterViewTemplate = `//go:build kyse

package auth

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// RegisterData is what the sign-up screen draws.
type RegisterData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Register</h1>
			</header>

			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .RegisterURL }}">
				@csrf

				{!! components.Field(components.FieldProps{
					Name: "name", Label: "Name",
					Value: .Name, Page: .,
					Autocomplete: "name", Required: true, Autofocus: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "email", Label: "Email", Type: "email",
					Value: .Email, Page: .,
					Autocomplete: "email", Required: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "password", Label: "Password", Type: "password",
					Page: .,
					Hint: "At least twelve characters.",
					Autocomplete: "new-password", Required: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "password_confirmation", Label: "Confirm password", Type: "password",
					Page: .,
					Autocomplete: "new-password", Required: true,
				}) !!}

				<div class="flex items-center justify-between gap-3">
					<button type="submit" class="btn">Register</button>
					<a class="text-muted-foreground text-sm hover:underline" href="{{ .LoginURL }}">Already registered?</a>
				</div>
			</form>
		</section>
	</div>
@endsection
`

// authVerifyViewTemplate is the "check your email" screen.
//
// It draws EmailError as well as Resent, because the same view answers a click
// on a link that did not work. The verify handler writes that field on three
// paths -- forged, expired, and an account the link no longer names -- and with
// no block to draw it, somebody clicking a dead link was shown the cheerful
// "check your inbox" page and no reason at all.
const authVerifyViewTemplate = `//go:build kyse

package auth

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// VerifyData is what the verification notice draws.
type VerifyData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Verify your email</h1>
			</header>

			<div class="flex flex-col gap-4 px-6 py-6 text-sm">
				@if(.Resent)
					{!! components.Alert(components.AlertProps{
						Title: "A fresh code is on its way",
						Message: "Check the address you registered with.",
					}) !!}
				@endif

				@if(.Status != "")
					{!! components.Alert(components.AlertProps{Title: .Status}) !!}
				@endif

				<p class="text-muted-foreground">Type the single-use code sent to your email address.</p>

				<form class="flex flex-col gap-4" method="post" action="{{ .VerificationConfirmURL }}">
					@csrf
					{!! components.Field(components.FieldProps{
						Name: "email", Label: "Email", Type: "email",
						Value: .Email, Page: ., Autocomplete: "email", Required: true,
					}) !!}
					{!! components.Field(components.FieldProps{
						Name: "email_code", Label: "Email code",
						Page: ., Autocomplete: "one-time-code", Required: true, Autofocus: true,
					}) !!}
					<div class="flex items-center gap-3">
						<button type="submit" class="btn">Confirm address</button>
						<button type="submit" class="btn" data-variant="outline" formaction="{{ .VerificationResendURL }}">Send another code</button>
					</div>
				</form>
			</div>
		</section>
	</div>
@endsection
`

// authPasswordEmailViewTemplate asks for the address to send the reset code to.
const authPasswordEmailViewTemplate = `//go:build kyse

package passwords

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// EmailData is what the "send me a code" screen draws.
type EmailData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Reset your password</h1>
			</header>

			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .PasswordEmailURL }}">
				@csrf

				@if(.Status != "")
					{!! components.Alert(components.AlertProps{Title: .Status}) !!}
				@endif

				{!! components.Field(components.FieldProps{
					Name: "email", Label: "Email", Type: "email",
					Value: .Email, Page: .,
					Hint: "We will send a code if the address is registered.",
					Autocomplete: "email", Required: true, Autofocus: true,
				}) !!}

				<div>
					<button type="submit" class="btn">Send the code</button>
				</div>
			</form>
		</section>
	</div>
@endsection
`

// authPasswordResetViewTemplate is the form the link in the email leads to.
const authPasswordResetViewTemplate = `//go:build kyse

package passwords

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// ResetData is what the new-password screen draws.
type ResetData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Choose a new password</h1>
			</header>

			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .PasswordUpdateURL }}">
				@csrf
				@if(.Status != "")
					{!! components.Alert(components.AlertProps{Title: .Status}) !!}
				@endif
				{!! components.Field(components.FieldProps{
					Name: "email", Label: "Email", Type: "email",
					Value: .Email, Page: .,
					Autocomplete: "email", Required: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "email_code", Label: "Email code",
					Page: .,
					Autocomplete: "one-time-code", Required: true, Autofocus: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "password", Label: "New password", Type: "password",
					Page: .,
					Hint: "At least twelve characters.",
					Autocomplete: "new-password", Required: true,
				}) !!}

				{!! components.Field(components.FieldProps{
					Name: "password_confirmation", Label: "Confirm the new password", Type: "password",
					Page: .,
					Autocomplete: "new-password", Required: true,
				}) !!}

				<div>
					<button type="submit" class="btn">Change it</button>
				</div>
			</form>
		</section>
	</div>
@endsection
`

const authTwoFactorChallengeViewTemplate = `//go:build kyse

package twofactor

import (
	"github.com/arandu-io/kyse/components"
	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
type ChallengeData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Two-factor authentication</h1>
			</header>
			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .TwoFactorChallengeURL }}">
				@csrf
				<p class="text-muted-foreground text-sm">Type the code from your authenticator application.</p>
				{!! components.Field(components.FieldProps{
					Name: "authenticator_code", Label: "Authenticator code",
					Page: ., Autocomplete: "one-time-code", Required: true, Autofocus: true,
				}) !!}
				<div class="flex items-center justify-between gap-3">
					<button type="submit" class="btn">Continue</button>
					<a class="text-muted-foreground text-sm hover:underline" href="{{ .TwoFactorRecoveryURL }}">Use a recovery code</a>
				</div>
			</form>
		</section>
	</div>
@endsection
`

const authTwoFactorRecoveryViewTemplate = `//go:build kyse

package twofactor

import (
	"github.com/arandu-io/kyse/components"
	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
type RecoveryData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Use a recovery code</h1>
			</header>
			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .TwoFactorRecoveryURL }}">
				@csrf
				{!! components.Field(components.FieldProps{
					Name: "recovery_code", Label: "Recovery code",
					Page: ., Autocomplete: "off", Required: true, Autofocus: true,
				}) !!}
				<div class="flex items-center justify-between gap-3">
					<button type="submit" class="btn">Continue</button>
					<a class="text-muted-foreground text-sm hover:underline" href="{{ .TwoFactorChallengeURL }}">Use an authenticator code</a>
				</div>
			</form>
		</section>
	</div>
@endsection
`

const authTwoFactorSetupViewTemplate = `//go:build kyse

package twofactor

import (
	"github.com/arandu-io/kyse/components"
	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
type SetupData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Set up two-factor authentication</h1>
			</header>
			<div class="flex flex-col gap-4 px-6 py-6">
				@if(.AuthenticatorCodeError != "")
					{!! components.Alert(components.AlertProps{Title: .AuthenticatorCodeError, Variant: "destructive"}) !!}
				@endif
				@if(.SecretKey == "")
					<p class="text-muted-foreground text-sm">Start setup to create a new authenticator secret.</p>
					<form method="post" action="{{ .TwoFactorSetupURL }}">
						@csrf
						<button type="submit" class="btn">Start setup</button>
					</form>
				@endif
				@if(.SecretKey != "")
					<div class="mx-auto max-w-64">{!! authui.TrustedQRCode(.QRCodeSVG) !!}</div>
					<p class="text-muted-foreground text-sm">If the camera cannot scan the code, type this key:</p>
					<code class="rounded border p-3 text-sm break-all">{{ .SecretKey }}</code>
					<form class="flex flex-col gap-4" method="post" action="{{ .TwoFactorSetupConfirmURL }}">
						@csrf
						{!! components.Field(components.FieldProps{
							Name: "authenticator_code", Label: "Authenticator code",
							Page: ., Autocomplete: "one-time-code", Required: true, Autofocus: true,
						}) !!}
						<button type="submit" class="btn">Confirm setup</button>
					</form>
				@endif
				<form method="post" action="{{ .TwoFactorDisableURL }}">
					@csrf
					<button type="submit" class="btn" data-variant="destructive">Disable two-factor authentication</button>
				</form>
			</div>
		</section>
	</div>
@endsection
`

const authRecoveryCodesViewTemplate = `//go:build kyse

package twofactor

import authui "<% .ModulePath %>/app/Http/Controllers/Auth"

@go
type RecoveryCodesData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Recovery codes</h1>
			</header>
			<div class="flex flex-col gap-4 px-6 py-6">
				<p class="text-muted-foreground text-sm">Store these codes somewhere safe. Each code works once.</p>
				<pre class="overflow-x-auto rounded border p-4 text-sm">{{ .RecoveryCodesText }}</pre>
				<form method="post" action="{{ .RecoveryCodesURL }}">
					@csrf
					<button type="submit" class="btn" data-variant="outline">Generate new recovery codes</button>
				</form>
			</div>
		</section>
	</div>
@endsection
`

// authPasswordConfirmViewTemplate asks for the password again before something
// that matters.
const authPasswordConfirmViewTemplate = `//go:build kyse

package passwords

import (
	"github.com/arandu-io/kyse/components"

	authui "<% .ModulePath %>/app/Http/Controllers/Auth"
)

@go
// ConfirmData is what the password-confirmation screen draws.
type ConfirmData = authui.AuthPage
@endgo

@extends('layouts.app')

@section('content')
	<div class="mx-auto w-full max-w-md">
		<section class="card">
			<header class="border-b px-6 py-4">
				<h1 class="text-base font-semibold tracking-tight">Confirm your password</h1>
			</header>

			<form class="flex flex-col gap-4 px-6 py-6" method="post" action="{{ .PasswordConfirmURL }}">
				@csrf

				<p class="text-muted-foreground text-sm">This is a protected area. Confirm your password before going on.</p>

				{!! components.Field(components.FieldProps{
					Name: "password", Label: "Password", Type: "password",
					Page: .,
					Autocomplete: "current-password", Required: true, Autofocus: true,
				}) !!}

				<div>
					<button type="submit" class="btn">Confirm</button>
				</div>
			</form>
		</section>
	</div>
@endsection
`

// screensOnly is what --views publishes.
//
// It refreshes the screens and leaves the backend the project has edited alone
// -- except that it cannot be quite that, and the reason is kyse: a page
// renders with the TYPE OF ITS LAYOUT, so the layout, page.go, home, welcome
// and HomeController are one unit. Publishing a new layout without the
// controller that hands it its data leaves a project that does not compile --
// which is why `replaced` exists in publish.go and lists exactly those five.
//
// So --views is the screens plus that unit, and the flag's help says so. The
// alternative -- refreshing login and register while a stale HomeController
// still hands over its own struct -- is a build failure delivered by a flag
// whose whole purpose is to be the safe one.
//
// What it leaves alone is the flow: the five authentication controller files
// and the two mailables, which are the files somebody edits to decide their own
// rules.
func screensOnly(files []File) []File {
	keep := map[string]bool{
		filepath.Join("app", "Http", "Controllers", "HomeController.go"): true,
		// render.go is the sixth member of that unit, and leaving it out made
		// --views the flag that writes code which does not compile: the
		// HomeController it publishes calls authui.Chrome and
		// authui.SignedInName, and both are declared here. A project that
		// reached for the safe flag before it had ever run the full command got
		// a build failure naming two symbols it had never heard of.
		//
		// It is safe to add because it is not in `replaced`: an existing
		// render.go is kept and reported as kept, exactly like the four
		// controllers below. Adding it changes what a project WITHOUT one gets,
		// and nothing else.
		filepath.Join("app", "Http", "Controllers", "Auth", "render.go"): true,
	}

	var out []File
	for _, f := range files {
		if keep[f.Path] || strings.HasPrefix(f.Path, filepath.Join("resources", "views")) ||
			f.Path == filepath.Join("app", "Http", "Controllers", "Auth", "page.go") {
			out = append(out, f)
		}
	}
	return out
}

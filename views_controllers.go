package main

import (
	"path/filepath"
)

// GenerateAuth produces the sign-in screens in the project.
//
// This is the starter kit, and it follows the Breeze contract literally: it
// publishes views, routes and handlers into the project and gets out of the way.
// Nothing here is a runtime dependency, and every file is yours to edit the
// moment it lands.
//
// It is a command rather than a package to import, because an imported starter
// kit cannot be edited -- and editing the sign-in screen is the first thing
// anyone does. It is a command rather than a repository to clone, because the
// generator already exists and a second delivery mechanism would be a second way
// to do one thing (RULE 9).
func GenerateAuth(m Module) ([]File, error) {
	if m.ModulePath == "" {
		return nil, errModulePath
	}

	// The controllers, at the conventional path: app/Http/Controllers/Auth.
	var out []File
	for _, t := range []struct {
		path string
		tmpl string
	}{
		{filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go"), authModuleTemplate},
		{filepath.Join("app", "Http", "Controllers", "Auth", "LoginController_handlers.go"), authHandlersTemplate},

		// The rest of the flow. Without these three, register.kyse.go and
		// verify.kyse.go post to addresses nobody registered, and the password
		// reset stops one step short of writing the password -- nine screens
		// that look like a kit and are not one.
		{filepath.Join("app", "Http", "Controllers", "Auth", "RegisterController.go"), authRegisterControllerTemplate},
		{filepath.Join("app", "Http", "Controllers", "Auth", "PasswordController.go"), authPasswordControllerTemplate},
		{filepath.Join("app", "Http", "Controllers", "Auth", "render.go"), authRenderTemplate},

		// The two messages the flow sends. A mailable that names a view the
		// project does not have is a link nobody receives, so they travel with
		// the four bodies AuthViews publishes.
		{filepath.Join("app", "Mail", "VerifyEmail.go"), verifyMailableTemplate},
		{filepath.Join("app", "Mail", "PasswordReset.go"), passwordMailableTemplate},
		// HomeController comes with the kit, and it is not optional.
		//
		// It is not optional here. A page renders with its layout's type, so
		// replacing the layout replaces what home passes -- and the skeleton's
		// controller, still handing over the struct its own view used to
		// declare, stops compiling. The controller and the view it renders are
		// one unit, and the kit owns both.
		{filepath.Join("app", "Http", "Controllers", "HomeController.go"), authHomeControllerTemplate},
	} {
		content, err := render(filepath.Base(t.path), t.tmpl, m)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: t.path, Content: content})
	}

	// And the nine views, which are the point of the command: the screens every
	// application has, at the paths people look for. Inside them it is kyse,
	// Tailwind and HTMX -- typed markup, utilities and hypermedia (ADR 0022).
	views, err := AuthViews(m)
	if err != nil {
		return nil, err
	}
	out = append(out, views...)
	return out, nil
}

// There is no manifest template here, and that is deliberate.
//
// One stood in this file declaring `[permissions] network = false, filesystem =
// false, exec = false, migrations = false` for the published module. It was
// referenced by nothing: GenerateAuth never wrote it, so no project ever had it,
// so `aru doctor` never read it -- a permissions declaration that looked
// load-bearing in review and enforced nothing anywhere.
//
// It is deleted rather than wired because there is nowhere honest to write it.
// The kit publishes into a project that already has its own arandu.toml, and a
// second manifest arriving beside it is a second place that declares what this
// application may do (RULE 9). The screens' permissions are the project's, and
// the project already states them.

const authModuleTemplate = `// Package authui holds the sign-in screens.
//
// Published by "go run github.com/arandu-io/ui@latest auth", and yours from
// that moment on: edit it, delete what you do not want, restyle it. Nothing in
// the framework imports it back.
//
// Not "aru make:auth". That command does not exist and will not: the artisan
// dropped its equivalent at version 6, for the reason RULE 3 repeats -- two
// ways to install one scaffolding diverge, and the second one is the one nobody
// maintains.
//
// The authentication itself stays in the framework's auth module -- password
// verification, the timing-equalized failure path, session rotation. This package
// owns what a screen owns, which is the markup and the two handlers that drive it.
package authui

import (
	"github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/kernel"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/security"
)

// Module registers the screens.
//
// It composes the framework's auth module instead of replacing it. Replacing it
// took the users table out of the schema: this kit declares no table of its own
// -- deliberately, because two owners for one table is how a rollout deadlocks
// -- so a wiring list with this module and without the framework's had nothing
// left to create users, and the seeder that makes the first administrator ran
// against a database with no such table.
//
// Embedding it means the schema, the health check and anything the framework
// adds to that module arrive with this one. What the kit takes over is Routes,
// and only Routes: the published screens answer where the minimal ones did.
type Module struct {
	// The framework's auth module. It is the owner of the users table, and it
	// stays the owner.
	*auth.Module

	auth     *auth.Service
	sessions *security.SessionStore
	csrf     *security.CSRF
	tenant   auth.TenantResolver

	// mailer is what sends the reset link, and base is the origin the link is
	// built on. The origin comes from configuration and never from the request:
	// a Host header is what the client sent, and a reset link built from one is
	// a reset link an attacker chose the destination of.
	mailer *mail.Mailer
	base   string
	// appName is what the header says. It comes from the configuration, because
	// the alternative shipped: a literal here, a different literal in the
	// handlers, and neither the one in config/app.go.
	appName string

	// signer is what a verification link is made of. The same application key
	// as the session and the CSRF token, because they are the same secret --
	// an attacker who has it does not need three.
	signer *security.Signer
}

// New returns the module.
//
// Register it INSTEAD of auth.New: this one already carries what auth.New
// returns, and both answer /auth/login. Registering both is a duplicate route
// and a table with two owners.
//
// The session store and the CSRF issuer are passed in rather than reached
// through the service, because a screen is allowed to know about a token and a
// cookie, and the service is not allowed to expose its own dependencies.
func New(svc *auth.Service, sessions *security.SessionStore, csrf *security.CSRF, mailer *mail.Mailer, appKey []byte, appName, base string, tenant auth.TenantResolver) *Module {
	if tenant == nil {
		tenant = auth.FixedTenant("")
	}
	return &Module{
		Module:   auth.New(svc, tenant),
		auth:     svc,
		sessions: sessions,
		csrf:     csrf,
		mailer:   mailer,
		base:     base,
		appName:  appName,
		signer:   security.NewSigner(appKey),
		tenant:   tenant,
	}
}

// Compile-time proof that the module honors the contracts it claims. The second
// line is the one that matters here: it is what stops a refactor that drops the
// embedded module and takes the users table down with it.
var (
	_ kernel.Module     = (*Module)(nil)
	_ kernel.Migratable = (*Module)(nil)
)

// Name is the module identifier. The routes below are this package's, so the
// route table names this package rather than the framework's.
func (m *Module) Name() string { return "authui" }

// Routes registers the screens, in place of the framework's.
//
// This is the one method the kit overrides. The embedded module still declares
// the users table; it just no longer answers /auth/login, because this does.
//
// guest is the guard on the two screens that exist to bring somebody in. It is
// on the route rather than at the top of each handler because there are two
// handlers today and there is a third the day somebody adds one -- and the
// third is the one that would render the sign-in form to a person who is
// already signed in, which reads to them as having been signed out.
//
// The framework's auth module guards its own minimal sign-in screen the same
// way, and this method replaces that one. The guard was missing here while it
// was already there, so publishing the kit TOOK A GUARD AWAY from the project
// it was published into -- and publishing again over a project that had added
// one by hand took it away a second time.
func (m *Module) Routes(r *fhttp.Router) {
	g := r.Group("/auth")

	// Where somebody already signed in is sent instead. The front page, which
	// is where signing in lands them too, so the two agree.
	guest := middleware.RedirectIfAuthenticated(m.sessions, "/")

	g.Get("/login", m.showLogin, guest)
	g.Post("/login", m.doLogin)
	g.Post("/logout", m.doLogout)

	// arandu:begin custom
	// The password reset, in PasswordController.go. The kit publishes the three
	// screens and stops there (ADR 0022): the handlers write to your users
	// table and send through your mailer, so they are yours.
	g.Get("/password", m.showPasswordRequest)
	g.Post("/password/email", m.sendPasswordLink)
	g.Get("/password/reset", m.showPasswordReset)
	g.Post("/password/update", m.updatePassword)

	// Typing the password again on a session that is already open. The screen
	// was published from the beginning with no route, no handler and nothing
	// assigning PasswordConfirmURL -- so it rendered action="" and posted to
	// itself, on a kit whose own instructions say every screen has a route.
	//
	// Behind the session guard, because there is nothing to confirm without one:
	// a post here from a guest would otherwise reach a handler that has to
	// invent an answer. The address is middleware.PasswordConfirmPath, which is
	// where middleware.RequireConfirmedPassword sends people -- mount that on
	// your own sensitive routes and this is the screen they land on.
	signedIn := middleware.RequireAuth(m.sessions)
	g.Get("/password/confirm", m.showPasswordConfirm, signedIn)
	g.Post("/password/confirm", m.confirmPassword, signedIn)

	// Registration and address verification, in RegisterController.go.
	//
	// /verify is the notice and /verify/confirm is the link. Two addresses and
	// not one with a branch: the notice is reached by a redirect after
	// registering, and the link arrives from a mail client -- and a GET that
	// sometimes changes state and sometimes does not is one nobody can cache,
	// log or reason about.
	g.Get("/register", m.showRegister, guest)
	g.Post("/register", m.doRegister)
	g.Get("/verify", m.showVerifyNotice)
	g.Get("/verify/confirm", m.verify)
	g.Post("/verify/resend", m.resendVerification)
	// arandu:end custom
}
`

const authHandlersTemplate = `package authui

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
)

// Handlers are thin on purpose: extract the input, delegate to the service,
// render. No business rule and no repository access lives here -- aru doctor
// complains when a handler imports the data package.

// showLogin renders the form.
//
// Through screen, like every other page of the kit, and that is the fix rather
// than the tidy-up it looks like. Rendering directly skipped the one place that
// fills in RegisterURL and PasswordRequestURL -- so the sign-in screen had no
// way to register and no way to recover a password, and the markup for both was
// sitting in the view behind an @if that was never true.
func (m *Module) showLogin(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.login", AuthPage{Page: m.page(r, "Sign in")})
}

// doLogin authenticates and rotates the session.
//
// On failure it re-renders the form with the message inline, which is the whole
// shape of validation on this stack: the server rejects, and the same response
// carries the reason. Nothing is validated twice in the browser.
func (m *Module) doLogin(w http.ResponseWriter, r *http.Request) {
	in := auth.LoginRequest{
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}

	// The box on the form, read here and nowhere else. The screen has drawn it
	// since the first version of this kit and nothing read it: AuthPage.Remember
	// was never assigned, so RememberAttribute() always answered "" and the box
	// did not even survive a rejected sign-in -- somebody who mistyped their
	// password had to tick it again, having no way to know it had been ignored
	// the first time.
	remember := r.PostFormValue("remember") != ""

	if errs := in.Validate(); errs.Any() {
		m.rejected(w, r, in.Email, remember, errs, http.StatusUnprocessableEntity)
		return
	}

	// The tenant comes from the application, never from the request: a form
	// field here would let anyone pick which tenant to authenticate against.
	//
	// The last argument is where the attempt came from, and it is what the
	// framework keys the sign-in throttle on. Read from the socket and never
	// from X-Forwarded-For: that header is written by whoever is calling, so an
	// attacker keyed on it resets their own counter every request.
	u, err := m.auth.Authenticate(r.Context(), m.tenant(r), in.Email, in.Password, middleware.KeyByIP(r))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// One message for both halves. Saying which one was wrong turns this
			// endpoint into an account enumeration oracle.
			m.rejected(w, r, in.Email, remember, validation.Errors{
				"email": {"invalid email or password"},
			}, http.StatusUnauthorized)
			return
		}
		// Too many failures for this address and this account. The counting
		// happens in the framework's auth service, not here -- this file is
		// yours to edit and to delete, and a lockout that a redesign of the
		// screen can remove is not a lockout. What is left for the screen is
		// saying how long, which is the part that stops somebody pressing the
		// button four more times.
		var locked auth.TooManyAttemptsError
		if errors.As(err, &locked) {
			w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
			m.rejected(w, r, in.Email, remember, validation.Errors{
				"email": {fmt.Sprintf("too many attempts, try again in %d seconds", locked.Seconds())},
			}, http.StatusTooManyRequests)
			return
		}
		observability.Log(r.Context()).Error("login failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Rotating the id is mandatory here: keeping the pre-login session is
	// session fixation, and aru doctor checks that this call exists.
	//
	// The remember-me answer travels with it, as security.Remember: Rotate is
	// what a sign-in calls, so an option only Start accepted would be unreachable
	// from the one screen that has the checkbox on it. A session started with it
	// lives for security.RememberLifetime instead of the store's ttl.
	old := m.sessions.IDFromRequest(r)
	if _, err := m.sessions.Rotate(r.Context(), w, old, auth.SubjectOf(u), security.Remember(remember)); err != nil {
		observability.Log(r.Context()).Error("starting session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Where they were going before a guard turned them away, and the front page
	// when there was nowhere in particular. middleware.RequireAuth is the only
	// thing that knows: by the time a password has been typed, the request that
	// was refused is gone.
	//
	// This used to be "/", and the framework's own sign-in handler already ended
	// this way -- so publishing the kit TOOK THE FEATURE AWAY from the project it
	// was published into, in the same shape as the guest guard did. The guards
	// went on writing the address on every refusal and nothing ever spent it:
	// somebody who followed a link to one invoice signed in, landed on the front
	// page, and went to find it again.
	//
	// The destination is proved local by the store, which is why this is one line
	// and not a check. An unchecked one would be an open redirect on the one
	// screen every application has.
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
}

// doLogout destroys the session on the server, not only in the browser.
func (m *Module) doLogout(w http.ResponseWriter, r *http.Request) {
	if err := m.sessions.Destroy(r.Context(), w, m.sessions.IDFromRequest(r)); err != nil {
		observability.Log(r.Context()).Error("destroying session", "error", err)
	}
	redirect(w, r, "/auth/login")
}

// redirect answers the way the request asked to be answered.
//
// The form is submitted two ways on purpose: hx-post when HTMX is running, and
// method="post" when it is not -- that is the path without JavaScript, and it is
// the one that has to keep working. HX-Redirect is a header only HTMX reads, so
// answering it to a plain form post is 200 with an empty body: a blank page
// after a login that succeeded, with nothing in the log to say so.
//
// fhttp.Redirect is where that branch already lives -- HX-Redirect for an HTMX
// request, 303 with a Location for everything else. Restating it here would be a
// second way to redirect, and the second one is the one that drifts (RULE 9).
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	fhttp.Redirect(w, r, to)
}

// rejected re-renders just the form, which is what HTMX swaps back in.
//
// The token is reissued because the session id may have changed, and the email
// and the remember-me box are kept because retyping either after a rejection is
// the fastest way to make a login screen unpleasant -- and a box that quietly
// unticks itself is worse than an empty field, because nothing on screen says it
// happened. The password never comes back.
func (m *Module) rejected(w http.ResponseWriter, r *http.Request, email string, remember bool, errs validation.Errors, status int) {
	// The status is explicit: HTMX swaps the body of a 422 and of a 200 alike,
	// so answering 200 for a rejection would make the browser, the logs and
	// every metric agree that it worked.
	m.screenStatus(w, r, status, "auth.login", AuthPage{
		Page:       m.page(r, "Sign in"),
		Email:      email,
		Remember:   remember,
		EmailError: errs.First(),
	})
}

// arandu:begin custom
// Your own handlers go here.
// arandu:end custom
`

// The views are no longer here. `aru make:auth` writes the nine of
// the starter kit in resources/views, as kyse — see views.go and ADR 0022.

// authHomeControllerTemplate is the HomeController the kit publishes, the same
// file the starter kit generates.
//
// The constructor takes the session store and the CSRF issuer, which the
// skeleton's did not: the layout this command installs draws a sign-out form and
// puts the token in hx-headers, and a controller that cannot read the session
// renders a landing page that says "Login" to somebody who just signed in, with
// an empty token that makes the next write fail the CSRF check. That is one line
// of wiring in bootstrap/app.go, and make:auth prints it -- the same shape
// `aru make:module` uses for every controller it writes.
//
// It also takes the auth service and the tenant, and that is the signature the
// skeleton declares too. It has to be: this file is in `replaced`, so a publish
// overwrites it with no flag at all, and a constructor here that the project's
// bootstrap/app.go does not call is a build that breaks on a command whose whole
// promise is that it can be run again. The kit, the skeleton and the showcase
// agree on the parameter list; the command prints the line that wires it, and
// TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes keeps the two
// from drifting apart.
const authHomeControllerTemplate = `package controllers

import (
	"github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/security"

	authui "{{ .ModulePath }}/app/Http/Controllers/Auth"
)

// HomeController answers the landing page.
//
// It renders with authui.AuthPage, the struct the auth controllers own. Every
// screen of the kit names that one type, in a single line, so a field this page
// does not use stays at its zero value rather than being a struct of its own.
type HomeController struct {
	Controller

	// appName is what the page is titled. It arrives through the constructor
	// rather than through a global read: a controller that reads the
	// environment is a controller no test can pin.
	appName string

	// sessions and csrf are what the chrome is drawn from: who is signed in,
	// and the token every write of this session carries. They arrive through
	// the constructor for the same reason appName does, and they are the same
	// two every controller ` + "`aru make:module`" + ` writes takes -- a screen is
	// allowed to know about a token and a cookie.
	sessions *security.SessionStore
	csrf     *security.CSRF

	// people and tenant are how the id in a session becomes a name to greet.
	// A session carries an id and not a name on purpose -- a name kept in one
	// stays wrong after somebody changes theirs -- so the header costs one
	// lookup by primary key, and the page greeted people with a UUID until it
	// had somewhere to make it.
	//
	// The tenant is whose rows are read (RULE 14). It comes from the
	// configuration, through bootstrap/app.go, and never from the request.
	people *auth.Service
	tenant string
}

// NewHomeController returns the controller. bootstrap/app.go builds it and hands
// it to the routes.
func NewHomeController(appName string, sessions *security.SessionStore, csrf *security.CSRF, people *auth.Service, tenant string) *HomeController {
	return &HomeController{
		appName: appName, sessions: sessions, csrf: csrf,
		people: people, tenant: tenant,
	}
}

// Compile-time proof that this controller answers GET / the way Resource and the
// route table expect. It costs nothing and catches a renamed method.
var _ fhttp.Indexer = (*HomeController)(nil)

// Index renders the landing page.
//
// The session and the token are read above the custom block, and deliberately:
// they are what the layout draws its navigation and its hx-headers from, so a
// regeneration that carried over an edited block would otherwise carry over a
// page that greets a signed-in visitor with a sign-in link.
func (c *HomeController) Index(ctx *fhttp.Context) error {
	// Who is signed in, from the session cookie and never from the request. An
	// error here is the anonymous case -- no cookie, a forged one, or a session
	// that expired -- and the guest half of the navigation is what gets drawn.
	subject, err := c.sessions.Load(ctx.Ctx(), ctx.Request)
	signedIn := err == nil

	// The token reaches the markup twice: the hidden field of the sign-out form
	// and the hx-headers attribute on <body>. A page rendered without one
	// answers 200 and then refuses the next write with 419, which reads like a
	// broken session rather than a missing field.
	token, err := c.csrf.Issue(c.sessions.IDFromRequest(ctx.Request))
	if err != nil {
		return err
	}

	// arandu:begin custom
	// The name to greet, through the helper the kit's own screens greet with, so
	// this page and the nine beside it answer the same way when the lookup fails.
	name := authui.SignedInName(ctx.Ctx(), c.people, c.tenant, subject.ID)

	// Which of the two published screens the front page is.
	//
	// It used to be "home" for everybody, and home.kyse.go is a card headed
	// "Dashboard" that says "You are logged in." -- so an anonymous visitor to a
	// freshly published project was told they were signed in, on the first page
	// they saw. Meanwhile welcome.kyse.go was published, compiled, and drawn by
	// nothing in any project: the landing page with the Login and Register
	// buttons on it, unreachable, under a command that prints "every screen has a
	// route and every route has a handler".
	//
	// One address and two screens, decided by the session, which is the shape
	// welcome's own @if(.Authenticated) was written for.
	screen := "welcome"
	if signedIn {
		screen = "home"
	}

	return ctx.View(screen, authui.AuthPage{
		// The chrome, from the one function that fills it. This used to be a
		// view.Page literal written here, and a literal is a list of fields
		// somebody can forget: this one forgot the name, the register link and
		// the reset, on the screen everybody sees first.
		Page: authui.Chrome(authui.ChromeProps{
			AppName:       c.appName,
			Title:         c.appName,
			Path:          ctx.Request.URL.Path,
			Token:         token,
			Authenticated: signedIn,
			UserName:      name,
		}),

		// Where a signed-in person is sent. It is the front page for this kit,
		// because that is what doLogin redirects to and what this controller
		// answers; welcome.kyse.go draws a "Dashboard" button from it, and an
		// application that grows a panel of its own changes this one line.
		DashboardURL: "/",

		// The reset is wired: this kit publishes PasswordController, mounts its
		// three routes and mails a signed link. The link on the sign-in screen
		// is drawn because the handler behind it exists, which is the only
		// reason a link is ever drawn.
		HasPasswordReset: true,
	})
	// arandu:end custom
}
`

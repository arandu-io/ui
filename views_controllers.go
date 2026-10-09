package main

import (
	"path/filepath"
)

// GenerateAuth produces the sign-in screens in the project.
//
// This is the starter kit: it publishes views, routes and handlers into the
// project and gets out of the way. Nothing here is a runtime dependency, and
// every file is yours to edit the moment it lands.
//
// It is a command rather than a package to import, because an imported starter
// kit cannot be edited -- and editing the sign-in screen is the first thing
// anyone does. It is a command rather than a repository to clone, because the
// generator already exists and a second delivery mechanism would be a second way
// to do one thing.
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
		// reset stops one step short of writing the password -- thirteen screens
		// that look like a kit and are not one.
		{filepath.Join("app", "Http", "Controllers", "Auth", "RegisterController.go"), authRegisterControllerTemplate},
		{filepath.Join("app", "Http", "Controllers", "Auth", "PasswordController.go"), authPasswordControllerTemplate},
		{filepath.Join("app", "Http", "Controllers", "Auth", "TwoFactorController.go"), authTwoFactorControllerTemplate},
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

	// And the views, which are the point of the command: the screens every
	// application has, at the paths people look for. Inside them it is kyse,
	// Tailwind and HTMX -- typed markup, utilities and hypermedia.
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
// application may do. The screens' permissions are the project's, and the
// project already states them.

const authModuleTemplate = `// Package authui holds the authentication screens
// published into this application. The domain and schema stay in app/.
package authui

import (
	"context"
	stdhttp "net/http"

	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/security"
	twofactor "github.com/arandu-io/hesape/2fa"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/onetime"
	"github.com/arandu-io/hesape/session"

	models "{{ .ModulePath }}/app/Models"
)

// TenantResolver decides which application tenant owns a guest request.
type TenantResolver func(*stdhttp.Request) string

// FixedTenant returns the resolver for an application with one configured
// tenant. It is the same request path with a constant, not a second mode.
func FixedTenant(tenant string) TenantResolver {
	return func(*stdhttp.Request) string { return tenant }
}

// UserNames is the smallest account view the shared page chrome needs.
type UserNames interface {
	PublicNames(context.Context, nativeauth.Subject, []string) (map[string]string, error)
}

// Users is the application-owned authentication interface consumed by these
// controllers. Its implementation owns policies, Model queries and writes.
type Users interface {
	UserNames
	VerifyCredentials(context.Context, string, string, string, string) (models.User, error)
	Register(context.Context, string, string, string, string) (models.User, error)
	FindForAuthentication(context.Context, string, string) (models.User, error)
	Lookup(context.Context, string, string) (models.User, error)
	MarkVerified(context.Context, string, string, string) (models.User, bool, error)
	ResetPassword(context.Context, string, string, string, string, string) (models.User, error)
	ConfirmPassword(context.Context, nativeauth.Subject, string, string) error
}

// Factors is the application-owned second-factor interface. Hesape supplies
// the algorithms and contracts; the application supplies policy and storage.
type Factors interface {
	Required(context.Context, string, string) (bool, error)
	Begin(context.Context, nativeauth.Subject, string) (twofactor.Provisioning, error)
	Confirm(context.Context, nativeauth.Subject, string) ([]string, error)
	Disable(context.Context, nativeauth.Subject) error
	RegenerateRecoveryCodes(context.Context, nativeauth.Subject) ([]string, error)
	VerifyAuthenticator(context.Context, string, string, string) error
	ConsumeRecovery(context.Context, string, string, string) error
}

// Module registers application-owned authentication screens and no schema.
type Module struct {
	users    Users
	factors  Factors
	codes    onetime.CodeStore
	sessions *security.SessionStore
	csrf     *session.CSRF
	mailer   *mail.Mailer
	signer   *encryption.Signer
	flash    *session.Flash
	appName  string
	tenant   TenantResolver
	secure   bool
}

// New returns the authentication screen module.
//
// csrf is kept for the call bootstrap/app.go already makes, and no screen
// issues a token with it: each draws the one CSRFProtect put on the request
// context.
func New(users Users, factors Factors, codes onetime.CodeStore, sessions *security.SessionStore, csrf *session.CSRF, mailer *mail.Mailer, appKey []byte, appName string, tenant TenantResolver, secure bool) *Module {
	if tenant == nil {
		tenant = FixedTenant("")
	}
	return &Module{
		users: users, factors: factors, codes: codes, sessions: sessions,
		csrf: csrf, mailer: mailer, signer: encryption.NewSigner(appKey),
		flash: session.NewFlash(appKey, secure),
		appName: appName, tenant: tenant, secure: secure,
	}
}

var _ foundation.Module = (*Module)(nil)

// Name is the module identifier.
func (m *Module) Name() string { return "authui" }

// Routes registers the twenty-three authentication routes.
//
// The eight that take a form somebody can get wrong are registered with Action,
// and the rest with Get and Post. An action returns validation.Errors and the
// router answers it: a page goes back where it came from with the messages and
// what was typed, and a client that asked for JSON gets a 422 problem document
// with the messages by field. No handler here writes a rejection itself.
//
// The router is handed this module's flash, the one its notices are written
// with, so a rejection and a notice travel in the same signed cookie however the
// router was built -- the kernel's carries a flash already, and a router a test
// builds carries none.
func (m *Module) Routes(r *fhttp.Router) {
	g := r.WithFlash(m.flash).Group("/auth")
	guest := middleware.RedirectIfAuthenticated(m.sessions, "/")
	signedIn := middleware.RequireAuth(m.sessions)
	confirmed := middleware.RequireConfirmedPassword(m.sessions)

	g.Get("/login", m.showLogin, guest).Name("auth.login")
	g.Action(stdhttp.MethodPost, "/login", m.doLogin, guest)
	g.Post("/logout", m.doLogout).Name("auth.logout")

	g.Get("/password", m.showPasswordRequest).Name("auth.password.request")
	g.Post("/password/email", m.sendPasswordCode).Name("auth.password.email")
	g.Get("/password/reset", m.showPasswordReset).Name("auth.password.reset")
	g.Action(stdhttp.MethodPost, "/password/update", m.updatePassword).Name("auth.password.update")
	g.Get("/password/confirm", m.showPasswordConfirm, signedIn).Name("auth.password.confirm")
	g.Action(stdhttp.MethodPost, "/password/confirm", m.confirmPassword, signedIn)

	g.Get("/register", m.showRegister, guest).Name("auth.register")
	g.Action(stdhttp.MethodPost, "/register", m.doRegister, guest)
	g.Get("/verify", m.showVerifyNotice).Name("auth.verify.notice")
	g.Action(stdhttp.MethodPost, "/verify/confirm", m.verify).Name("auth.verify.confirm")
	g.Post("/verify/resend", m.resendVerification).Name("auth.verify.resend")

	g.Get("/two-factor/challenge", m.showTwoFactorChallenge, guest).Name("auth.two-factor.challenge")
	g.Action(stdhttp.MethodPost, "/two-factor/challenge", m.verifyTwoFactorChallenge, guest)
	g.Get("/two-factor/recovery", m.showRecoveryChallenge, guest).Name("auth.two-factor.recovery")
	g.Action(stdhttp.MethodPost, "/two-factor/recovery", m.verifyRecoveryChallenge, guest)
	g.Get("/two-factor/setup", m.showTwoFactorSetup, signedIn, confirmed).Name("auth.two-factor.setup")
	g.Post("/two-factor/setup", m.beginTwoFactorSetup, signedIn, confirmed)
	g.Action(stdhttp.MethodPost, "/two-factor/setup/confirm", m.confirmTwoFactorSetup, signedIn, confirmed).Name("auth.two-factor.setup.confirm")
	g.Post("/two-factor/disable", m.disableTwoFactor, signedIn, confirmed).Name("auth.two-factor.disable")
	g.Post("/two-factor/recovery-codes", m.regenerateRecoveryCodes, signedIn, confirmed).Name("auth.two-factor.recovery-codes")

	// arandu:begin custom
	// Register application-specific authentication routes here.
	// arandu:end custom
}
`

const authHandlersTemplate = `package authui

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	nativeauth "github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/log"
	hmiddleware "github.com/arandu-io/hesape/routing/middleware"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"

	models "{{ .ModulePath }}/app/Models"
)

type retryAfterError interface {
	error
	Seconds() int
}

// showLogin renders the form.
//
// What an attempt that was turned away left behind -- the messages, the address
// that was typed, a notice from another screen -- is drawn by Module.screen from
// the flash. The box is the one field drawn by hand rather than by a component,
// so it is read here: a box that quietly unticks itself after a refused sign-in
// is worse than an empty field, because nothing on screen says it happened.
func (m *Module) showLogin(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.login", AuthPage{
		Page:     m.page(r, "Sign in"),
		Remember: hhttp.StateFrom(r.Context()).Old.Get("remember") != "",
	})
}

// doLogin validates the password without creating identity. A final session is
// written here only when no factor is enabled; otherwise a short pending cookie
// carries the attempt to TwoFactorController.
//
// A refused attempt is returned, never drawn. The router sends the person back
// to the form with the message and what was typed, minus the password; a reload
// of the page that follows asks for the form again rather than posting the
// password a second time.
func (m *Module) doLogin(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	remember := r.PostFormValue("remember") != ""
	if email == "" || password == "" {
		errs := validation.Errors{}
		if email == "" {
			errs["email"] = []string{"type your email address"}
		}
		if password == "" {
			errs["password"] = []string{"type your password"}
		}
		return errs
	}

	tenant := m.tenant(r)
	u, err := m.users.VerifyCredentials(r.Context(), tenant, email, password, hmiddleware.KeyByIP(r))
	if err != nil {
		if errors.Is(err, nativeauth.ErrInvalidCredentials) {
			return validation.Errors{"email": {"invalid email or password"}}
		}
		var locked retryAfterError
		if errors.As(err, &locked) {
			w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
			return validation.Errors{
				"email": {fmt.Sprintf("too many attempts, try again in %d seconds", locked.Seconds())},
			}
		}
		log.For(r.Context()).Error("login failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}

	required, err := m.factors.Required(r.Context(), u.TenantID, u.ID)
	if err != nil {
		log.For(r.Context()).Error("checking the second factor", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	if required {
		if err := m.writePending(w, u, remember); err != nil {
			log.For(r.Context()).Error("starting the second-factor challenge", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return nil
		}
		redirect(w, r, "/auth/two-factor/challenge")
		return nil
	}
	m.finishSignIn(w, r, u, remember)
	return nil
}

// finishSignIn is the only session-creation seam in the published flow. It is
// called after password-only success or after a factor succeeds, never between.
func (m *Module) finishSignIn(w http.ResponseWriter, r *http.Request, u models.User, remember bool) {
	old := m.sessions.IDFromRequest(r)
	if _, err := m.sessions.Rotate(r.Context(), w, old, subjectOf(u), session.Remember(remember)); err != nil {
		log.For(r.Context()).Error("starting session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.clearPending(w)
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
}

func subjectOf(u models.User) nativeauth.Subject {
	return nativeauth.Subject{ID: u.ID, Tenant: u.TenantID, Roles: u.Roles, Verified: u.Verified()}
}

// doLogout destroys the session on the server, not only in the browser.
func (m *Module) doLogout(w http.ResponseWriter, r *http.Request) {
	if err := m.sessions.Destroy(r.Context(), w, m.sessions.IDFromRequest(r)); err != nil {
		log.For(r.Context()).Error("destroying session", "error", err)
	}
	redirect(w, r, "/auth/login")
}

// redirect answers HX-Redirect under htmx and a 303 with a Location otherwise.
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	hhttp.Redirect(w, r, to)
}

// arandu:begin custom
// Your own handlers go here.
// arandu:end custom
`

// The views are no longer here. AuthViews writes the thirteen screens of the
// starter kit into resources/views, as kyse -- see views.go.

// authHomeControllerTemplate is the HomeController the kit publishes, the same
// file the starter kit generates.
//
// The constructor takes the application name, the application-owned name reader
// and the tenant, and nothing else. That is the signature the skeleton declares,
// and it has to be: this file is in `replaced`, so a publish overwrites it with
// no flag at all, and a constructor here that the project's bootstrap/app.go
// does not call is a build that breaks on a command whose whole promise is that
// it can be run again.
//
// There is no session store and no CSRF issuer among the parameters. The
// skeleton mounts GET / behind middleware.LoadSubject, so who is signed in is
// on the request, and CSRFProtect put the token there too; the page reads both
// off the request it is answering. The kit took both collaborators once, and
// publishing it into a project whose bootstrap/app.go called the three-argument
// constructor left that project unable to compile.
//
// TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes keeps the line
// the command prints in step with this constructor, and
// TestTheProjectsInThisTreeFitTheConstructorTheKitPublishes keeps the skeleton's.
const authHomeControllerTemplate = `package controllers

import (
	fhttp "github.com/arandu-io/framework/http"
	hhttp "github.com/arandu-io/hesape/http"

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

	// people and tenant are how the id in a session becomes a name to greet.
	// A session carries an id and not a name on purpose -- a name kept in one
	// stays wrong after somebody changes theirs -- so the header costs one
	// lookup by primary key, and the page greeted people with a UUID until it
	// had somewhere to make it.
	//
	// The tenant is whose rows are read. It comes from the configuration,
	// through bootstrap/app.go, and never from the request.
	people authui.UserNames
	tenant string
}

// NewHomeController returns the controller. bootstrap/app.go builds it and hands
// it to the routes.
//
// There is no session store and no CSRF issuer here: the route puts who is
// signed in on the request, and the middleware that protects forms puts the
// token there, so the page reads both off the request it is answering.
func NewHomeController(appName string, people authui.UserNames, tenant string) *HomeController {
	return &HomeController{appName: appName, people: people, tenant: tenant}
}

// Compile-time proof that this controller answers GET / the way Resource and the
// route table expect. It costs nothing and catches a renamed method.
var _ fhttp.Indexer = (*HomeController)(nil)

// Index renders the landing page.
//
// The subject and the token are read above the custom block, and deliberately:
// they are what the layout draws its navigation and its hx-headers from, so a
// regeneration that carried over an edited block would otherwise carry over a
// page that greets a signed-in visitor with a sign-in link.
//
// The route has to mount this behind middleware.LoadSubject, as the skeleton's
// routes/web.go does. Without it nothing puts a subject on the request, and
// every visitor is drawn the guest half.
func (c *HomeController) Index(ctx *hhttp.Context) error {
	// Who is signed in, put on the request by the route's LoadSubject from the
	// session cookie and never from the request body. No subject is the
	// anonymous case -- no cookie, a forged one, or a session that expired --
	// and the guest half of the navigation is what gets drawn.
	subject, signedIn := ctx.User()

	// The token reaches the markup twice: the hidden field of the sign-out form
	// and the hx-headers attribute on <body>. A page rendered without one
	// answers 200 and then refuses the next write with 419, which reads like a
	// broken session rather than a missing field. CSRFProtect issued it for
	// this request, bound to the session or, for a visitor with none, to their
	// guest cookie, and put it on the request context.
	token, _ := hhttp.CSRFTokenFrom(ctx.Ctx())

	// arandu:begin custom
	// The name to greet, through the helper the kit's own screens greet with, so
	// this page and the thirteen beside it answer the same way when the lookup fails.
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
		// routes and mails a single-use code. The link on the sign-in screen
		// is drawn because the handler behind it exists, which is the only
		// reason a link is ever drawn.
		HasPasswordReset: true,
	})
	// arandu:end custom
}
`

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
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/onetime"
	"github.com/arandu-io/hesape/session"
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

// Users is what these controllers ask of the application's accounts: the part
// of *account.Users the screens call. The application's provider builds that
// with account.NewUsers over its users query and user policy, and
// bootstrap/app.go hands it to New.
//
// Every method answers an account.Account, which is the application's
// *models.User built by its own users query. The handlers read it through the
// six readers the model implements, and the session subject through its
// Subject method (see subjectAccount).
type Users interface {
	UserNames
	VerifyCredentials(ctx context.Context, tenant, email, password, client string) (account.Account, error)
	Register(ctx context.Context, tenant string, registration account.Registration) (account.Account, error)
	FindForAuthentication(ctx context.Context, tenant, userID string) (account.Account, error)
	Lookup(ctx context.Context, tenant, email string) (account.Account, error)
	MarkVerified(ctx context.Context, tenant, userID, capturedEmail string) (account.Account, bool, error)
	ResetPassword(ctx context.Context, tenant, userID, capturedEmail, capturedFingerprint, newPassword string) (account.Account, error)
	ConfirmPassword(ctx context.Context, subject nativeauth.Subject, password, client string) error
}

// Factors is what these controllers ask of the authenticator second factor:
// the part of *account.Factors the screens call, built by account.NewFactors.
// It counts every code against the account's attempt budget, so nothing here
// checks a code itself.
type Factors interface {
	Required(ctx context.Context, tenant, userID string) (bool, error)
	Begin(ctx context.Context, actor nativeauth.Subject, issuer string) (twofactor.Provisioning, error)
	Confirm(ctx context.Context, actor nativeauth.Subject, code string) ([]string, error)
	Disable(ctx context.Context, actor nativeauth.Subject) error
	RegenerateRecoveryCodes(ctx context.Context, actor nativeauth.Subject) ([]string, error)
	VerifyAuthenticator(ctx context.Context, tenant, userID, code string) error
	ConsumeRecovery(ctx context.Context, tenant, userID, code string) error
}

// Compile-time proof that the account package answers both interfaces, so a
// method renamed there is a build error here rather than a nil at run time.
var (
	_ Users   = (*account.Users)(nil)
	_ Factors = (*account.Factors)(nil)
)

// Module registers application-owned authentication screens and no schema.
type Module struct {
	users    Users
	factors  Factors
	codes    onetime.CodeStore
	sessions *security.SessionStore
	csrf     *session.CSRF
	mailer   *mail.Mailer
	limiter  *cache.RateLimiter
	signer   *encryption.Signer
	flash    *session.Flash
	tenant   TenantResolver
	secure   bool

	// registration reads what this application's sign-up form adds to the
	// three fields the kit reads. Nil is the form the kit publishes. See
	// WithRegistration.
	registration RegistrationReader

	// withoutRemember withholds "remember me". The zero value offers it, which
	// is what these screens have always done. See WithRemember.
	withoutRemember bool
}

// New returns the authentication screen module.
//
// csrf is kept for the call bootstrap/app.go already makes, and no screen
// issues a token with it: each draws the one CSRFProtect put on the request
// context.
//
// limiter counts the forms that send or check a one-time code, per client and
// per address typed (see throttleCodeForm). Pass drivers.Limiter, which counts
// in the cache store the application's own throttle counts in, so every
// replica spends from one budget. Nil counts in this process alone: right for
// a single process and for a test, and N replicas would each allow the whole
// budget.
//
// secure is whether the cookies this module writes itself carry Secure: the
// short pending cookie of a two-factor sign-in, and the flash it builds for a
// router that was wired with none. Pass cfg.Framework.Session.Secure, the one
// decision the session cookie and the kernel's flash already follow.
//
// There is no application name here. The framework puts APP_NAME on every
// request, and the module reads it there -- hhttp.AppNameFrom -- for the brand
// on each screen, the signature of each message and the issuer an
// authenticator app files a second factor under. A name passed in as well
// would be a second answer to the same question.
func New(users Users, factors Factors, codes onetime.CodeStore, sessions *security.SessionStore, csrf *session.CSRF, mailer *mail.Mailer, limiter *cache.RateLimiter, appKey []byte, tenant TenantResolver, secure bool) *Module {
	if tenant == nil {
		tenant = FixedTenant("")
	}
	if limiter == nil {
		limiter = cache.NewRateLimiter(cache.NewArrayStore())
	}
	return &Module{
		users: users, factors: factors, codes: codes, sessions: sessions,
		csrf: csrf, mailer: mailer, limiter: limiter, signer: encryption.NewSigner(appKey),
		// Replaced in Routes by the router's flash when it carries one.
		flash: session.NewFlash(appKey, secure),
		tenant: tenant, secure: secure,
	}
}

// WithRemember offers or withholds "remember me" on the sign-in screen.
//
// Offered, which is what New leaves, the box is drawn and a ticked one starts
// the long-lived session. Withheld, the box is not drawn and a remember field
// that arrives anyway is ignored, so every sign-in ends with the browser. One
// value decides both, which is what keeps the screen from offering what the
// handler then refuses.
func (m *Module) WithRemember(offered bool) *Module {
	m.withoutRemember = !offered
	return m
}

var _ foundation.Module = (*Module)(nil)

// Name is the module identifier.
func (m *Module) Name() string { return "authui" }

// Routes registers the twenty-three authentication routes.
//
// The ten that read a form are registered with Action, and the rest with Get
// and Post. An action converts its form with ctx.Bind into a request struct
// declared beside it, and reads no field by hand. When it refuses the form it
// returns validation.Errors and the router answers it: a page goes back where it
// came from with the messages and what was typed, and a client that asked for
// JSON gets a 422 problem document with the messages by field. No handler here
// writes a rejection itself.
//
// A rejection and a notice travel in one signed cookie, the flash the
// application reads back on the next page. The kernel's router carries that
// flash, and when it does this module writes its notices with it rather than
// with one of its own, so the key and the Secure attribute are the ones the
// application decided once. A router that carries none -- one a test builds --
// is handed the flash New built, and both kinds of message go through it.
func (m *Module) Routes(r *fhttp.Router) {
	if f := flashOf(r); f != nil {
		m.flash = f
	} else {
		r = r.WithFlash(m.flash)
	}
	g := r.Group("/auth")
	guest := middleware.RedirectIfAuthenticated(m.sessions, "/")
	signedIn := middleware.RequireAuth(m.sessions)
	confirmed := middleware.RequireConfirmedPassword(m.sessions)

	g.Get("/login", m.showLogin, guest).Name("auth.login")
	g.Action(stdhttp.MethodPost, "/login", m.doLogin, guest)
	g.Post("/logout", m.doLogout).Name("auth.logout")

	g.Get("/password", m.showPasswordRequest).Name("auth.password.request")
	g.Action(stdhttp.MethodPost, "/password/email", m.sendPasswordCode).Name("auth.password.email")
	g.Get("/password/reset", m.showPasswordReset).Name("auth.password.reset")
	g.Action(stdhttp.MethodPost, "/password/update", m.updatePassword).Name("auth.password.update")
	g.Get("/password/confirm", m.showPasswordConfirm, signedIn).Name("auth.password.confirm")
	g.Action(stdhttp.MethodPost, "/password/confirm", m.confirmPassword, signedIn)

	g.Get("/register", m.showRegister, guest).Name("auth.register")
	g.Action(stdhttp.MethodPost, "/register", m.doRegister, guest)
	g.Get("/verify", m.showVerifyNotice).Name("auth.verify.notice")
	g.Action(stdhttp.MethodPost, "/verify/confirm", m.verify).Name("auth.verify.confirm")
	g.Action(stdhttp.MethodPost, "/verify/resend", m.resendVerification).Name("auth.verify.resend")

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

// flashCarrier is a router that says which flash it was wired with.
type flashCarrier interface {
	Flash() *session.Flash
}

// flashOf returns the flash the router was wired with, or nil.
//
// It asks through flashCarrier rather than calling r.Flash() so that this file
// still compiles against a framework whose router keeps its flash to itself;
// there the answer is nil and the module wires its own, as it always did.
func flashOf(r *fhttp.Router) *session.Flash {
	if c, ok := any(r).(flashCarrier); ok {
		return c.Flash()
	}
	return nil
}
`

const authHandlersTemplate = `package authui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
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

// loginRequest is what the sign-in form sends.
//
// ctx.Bind fills it through the form tags and nothing else: a key the form does
// not declare reaches no field, every value arrives trimmed, and an unticked
// box is false.
type loginRequest struct {
	Email    string ` + "`form:\"email\"`" + `
	Password string ` + "`form:\"password\"`" + `
	Remember bool   ` + "`form:\"remember\"`" + `
}

// LogValue says which fields arrived and nothing they carried: the address is
// account data and the password is the credential.
func (in loginRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("email_supplied", in.Email != ""),
		slog.Bool("password_supplied", in.Password != ""),
		slog.Bool("remember", in.Remember),
	)
}

// showLogin renders the form.
//
// What an attempt that was turned away left behind -- the messages, the address
// that was typed, a notice from another screen -- is drawn by Module.screen from
// the flash. The box is the one field drawn by hand rather than by a component,
// so it is read here: a box that quietly unticks itself after a refused sign-in
// is worse than an empty field, because nothing on screen says it happened.
// Whether the box is drawn at all is WithRemember's, and doLogin reads the same
// value.
func (m *Module) showLogin(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.login", AuthPage{
		Page:            m.page(r, "Sign in"),
		Remember:        !m.withoutRemember && hhttp.StateFrom(r.Context()).Old.Get("remember") != "",
		WithoutRemember: m.withoutRemember,
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
	var in loginRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Email == "" || in.Password == "" {
		errs := validation.Errors{}
		if in.Email == "" {
			errs["email"] = []string{"type your email address"}
		}
		if in.Password == "" {
			errs["password"] = []string{"type your password"}
		}
		return errs
	}
	// A box the screen did not draw was not ticked by anybody here.
	remember := in.Remember && !m.withoutRemember

	tenant := m.tenant(r)
	u, err := m.users.VerifyCredentials(r.Context(), tenant, in.Email, in.Password, hmiddleware.KeyByIP(r))
	if trimmed := strings.TrimSpace(in.Password); trimmed != in.Password && trimmed != "" && wrongPassword(err) {
		u, err = m.verifyTrimmedPassword(r, tenant, in.Email, trimmed)
	}
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

	required, err := m.factors.Required(r.Context(), u.AccountTenant(), u.AccountID())
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

// wrongPassword reports whether err is the plain refusal of a password, and not
// a lock that happens to match it as well.
func wrongPassword(err error) bool {
	var locked retryAfterError
	return errors.Is(err, nativeauth.ErrInvalidCredentials) && !errors.As(err, &locked)
}

// verifyTrimmedPassword is the transition for a password stored trimmed.
//
// ctx.Bind used to trim every value, the password included, so a password
// chosen with spaces at its ends through this kit's v0.21.0 handlers, on hesape
// v0.50.1 or earlier, was hashed without them. Bind now leaves a password as
// typed, and the person who types the spaces they chose would be refused.
// doLogin calls this only then: the typed password was refused, it is not a
// lock, and it differs from its trimmed form, which is not empty.
//
// It asks the user service again, with the trimmed form: the same check a
// sign-in makes, counted against the same throttle, held to the same
// Hooks.Admit, inside the same timebox whether an account answers or not. So a
// password typed with spaces that is wrong both ways counts as two attempts,
// and the trimmed form is never a way around the counter or around Admit.
//
// Nothing is written. The stored hash stays what it was and no event is
// stored, so the next sign-in, typed with the spaces or without them, passes
// the same way; a keyboard that adds a space at the end signs its owner in
// rather than changing their password. Every refusal but a lock -- a password
// that does not match, an account Admit refused, a failure of the service -- is
// answered as ErrInvalidCredentials, which doLogin answers with its one message.
//
// Delete it, with its call in doLogin, once no account can hold a password set
// while Bind trimmed: every password registered or reset through kit v0.21.0
// on hesape v0.50.1 or earlier. An application that never ran that pair has
// none and can delete it today.
func (m *Module) verifyTrimmedPassword(r *http.Request, tenant, email, trimmed string) (account.Account, error) {
	u, err := m.users.VerifyCredentials(r.Context(), tenant, email, trimmed, hmiddleware.KeyByIP(r))
	if err != nil {
		var locked retryAfterError
		if errors.As(err, &locked) {
			return nil, err
		}
		if !errors.Is(err, nativeauth.ErrInvalidCredentials) {
			log.For(r.Context()).Error("verifying the trimmed password", "error", err)
		}
		return nil, nativeauth.ErrInvalidCredentials
	}
	log.For(r.Context()).Info("login credentials verified in their trimmed form",
		"user_id", u.AccountID(), "tenant", u.AccountTenant())
	return u, nil
}

// finishSignIn is the only session-creation seam in the published flow. It is
// called after password-only success or after a factor succeeds, never between.
func (m *Module) finishSignIn(w http.ResponseWriter, r *http.Request, u account.Account, remember bool) {
	subject, err := sessionSubject(u)
	if err == nil {
		old := m.sessions.IDFromRequest(r)
		_, err = m.sessions.Rotate(r.Context(), w, old, subject, session.Remember(remember))
	}
	if err != nil {
		log.For(r.Context()).Error("starting session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.clearPending(w)
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
}

// subjectAccount is an account that builds the session subject it signs in
// as.
//
// The application's *models.User is one: its Subject method is the one place
// a subject is built from what is stored, and the session store's resolver
// calls the same method again on every request. A second builder here would
// be a second answer to who the person is and what they may do.
type subjectAccount interface {
	account.Account
	Subject() nativeauth.Subject
}

// The users query of this application answers *models.User, so it has to
// build its subject; a model without Subject is a build error here rather than
// a refused sign-in.
var _ subjectAccount = (*models.User)(nil)

// sessionSubject is the subject the account builds for itself. An account
// that builds none is refused, never given a subject guessed from its
// readers.
func sessionSubject(u account.Account) (nativeauth.Subject, error) {
	built, ok := u.(subjectAccount)
	if !ok {
		return nativeauth.Subject{}, fmt.Errorf("the account %T does not build its session subject", u)
	}
	return built.Subject(), nil
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

// The four forms that send or check a one-time code -- asking for a reset,
// choosing the new password, confirming an address and asking for another
// code -- are limited twice, apart from the application's own throttle: per
// address typed and per client. Both are counted before anything is looked up,
// so an address with no account runs out exactly when one with an account
// does, and running out says nothing about which it is.
//
// The address budget is what bounds guessing. A code allows a few tries and
// every new code starts its own, so whoever can ask for codes could otherwise
// guess for as long as they kept asking. Ten an hour is two hundred and forty a
// day against a million six-digit codes. It is also a budget somebody else can
// spend on an address, which costs its owner an hour; lowering it lowers both.
//
// The client budget is what bounds walking a list of addresses from one place.
// Raise either for a form your people really use more often than this.
var (
	codeFormLimitPerAddress = cache.PerHour(10)
	codeFormLimitPerClient  = cache.PerMinutes(15, 30)
)

// throttleCodeForm counts one submission of the form named form against the
// client that sent it and the address it carries, and refuses it under field
// once either has spent its budget.
//
// It fails closed: a store that cannot count is logged and the form is
// refused. The application's throttle lets a request through when its store is
// down, because it guards volume; this one is what keeps a code from being
// guessed, and letting the form through would take it away exactly then.
func (m *Module) throttleCodeForm(w http.ResponseWriter, r *http.Request, form, field, email string) error {
	limits := []cache.Limit{codeFormLimitPerClient.By("authui:" + form + ":" + hmiddleware.KeyByIP(r))}
	if address := account.NormalizeEmail(email); address != "" {
		// Hashed, so the cache keeps no address in a key.
		sum := sha256.Sum256([]byte(m.tenant(r) + "\x00" + address))
		limits = append(limits, codeFormLimitPerAddress.By("authui:"+form+":address:"+hex.EncodeToString(sum[:])))
	}
	for _, limit := range limits {
		result, err := m.limiter.Attempt(r.Context(), limit)
		if err != nil {
			log.For(r.Context()).Error("counting a code form against its limit", "form", form, "error", err)
			return validation.Errors{field: {"this form cannot be checked right now, try again in a minute"}}
		}
		if !result.OK {
			seconds := int(result.RetryAfter/time.Second) + 1
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			return validation.Errors{field: {tooManyAttempts(seconds)}}
		}
	}
	return nil
}

// tooManyAttempts is what a code form says once a budget is spent, in whole
// minutes rounded up.
func tooManyAttempts(seconds int) string {
	if minutes := (seconds + 59) / 60; minutes > 1 {
		return fmt.Sprintf("too many attempts, try again in %d minutes", minutes)
	}
	return "too many attempts, try again in a minute"
}

// codeRequestTime is how long asking for a code takes to answer, whether the
// address has an account or not.
//
// An address with an account is looked up, issued a code and mailed; one
// without is only looked up. Answered as soon as that work was done, the time
// alone would say which addresses have accounts, whatever the sentence says.
// Raise it if looking an account up and issuing a code take longer than this
// here. A slow mailer does not need it raised: the message goes on being sent
// after the answer.
const codeRequestTime = time.Second

// answerAfter runs work and returns when d has passed: not before, when work
// was quick, and not later, when it was slow.
//
// Work that outlasts d goes on after the answer is written, under a context
// the end of the request does not cancel, so a slow mailer delays a message
// rather than telling the time. It must not touch the request or the response.
func answerAfter(ctx context.Context, d time.Duration, work func(context.Context)) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(context.WithoutCancel(ctx))
	}()
	select {
	case <-done:
		<-timer.C
	case <-timer.C:
	}
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
// The constructor takes the application-owned name reader and the tenant, and
// nothing else. That is the signature the skeleton declares, and it has to be:
// this file is in `replaced`, so a publish overwrites it with no flag at all,
// and a constructor here that the project's provider does not call is a
// build that breaks on a command whose whole promise is that it can be run
// again.
//
// There is no session store, no CSRF issuer and no application name among the
// parameters. The skeleton mounts GET / behind middleware.LoadSubject, so who is
// signed in is on the request, CSRFProtect put the token there too, and the
// framework puts APP_NAME there before every route; the page reads all three
// off the request it is answering. The kit took the two collaborators once, and
// publishing it into a project whose wiring called the three-argument
// constructor left that project unable to compile. The name is read inside the
// custom block, never above it: a variable declared above that an older block
// carried over does not use is a build that breaks.
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

	// people and tenant are how the id in a session becomes a name to greet.
	// A session carries an id and not a name on purpose -- a name kept in one
	// stays wrong after somebody changes theirs -- so the header costs one
	// lookup by primary key, and the page greeted people with a UUID until it
	// had somewhere to make it.
	//
	// The tenant is whose rows are read. It comes from the configuration,
	// through the provider that builds this controller, and never from the
	// request.
	people authui.UserNames
	tenant string
}

// NewHomeController returns the controller. The application's provider builds
// it and hands it to the routes.
//
// There is no session store, no CSRF issuer and no application name here: the
// route puts who is signed in on the request, the middleware that protects
// forms puts the token there, and the framework puts APP_NAME there, so the
// page reads all three off the request it is answering.
func NewHomeController(people authui.UserNames, tenant string) *HomeController {
	return &HomeController{people: people, tenant: tenant}
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

	// The application's name, which titles this page as well as branding it.
	// The framework puts APP_NAME on every request, so it is read here, off the
	// request, and the constructor does not take it.
	appName := hhttp.AppNameFrom(ctx.Ctx())

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

	// The chrome, from the one function that fills it. This used to be a
	// view.Page literal written here, and a literal is a list of fields
	// somebody can forget: this one forgot the name, the register link and the
	// reset, on the screen everybody sees first.
	chrome := authui.Chrome(authui.ChromeProps{
		AppName:       appName,
		Title:         appName,
		Path:          ctx.Request.URL.Path,
		Token:         token,
		Authenticated: signedIn,
		UserName:      name,
	})

	// What the action that redirected here said it did, with ctx.With. The
	// layout draws it in its toaster, and it is read off this request because
	// it is this request's: an action that saves something and sends the person
	// to the front page says so on the front page.
	chrome.Messages = ctx.State().Messages

	return ctx.View(screen, authui.AuthPage{
		Page: chrome,

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

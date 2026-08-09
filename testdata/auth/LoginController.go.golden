// Package authui holds the sign-in screens.
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
	"github.com/arandu-io/framework/httpx"
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
func (m *Module) Routes(r *httpx.Router) {
	g := r.Group("/auth")
	g.Get("/login", m.showLogin)
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

	// Registration and address verification, in RegisterController.go.
	//
	// /verify is the notice and /verify/confirm is the link. Two addresses and
	// not one with a branch: the notice is reached by a redirect after
	// registering, and the link arrives from a mail client -- and a GET that
	// sometimes changes state and sometimes does not is one nobody can cache,
	// log or reason about.
	g.Get("/register", m.showRegister)
	g.Post("/register", m.doRegister)
	g.Get("/verify", m.showVerifyNotice)
	g.Get("/verify/confirm", m.verify)
	g.Post("/verify/resend", m.resendVerification)
	// arandu:end custom
}

package authui

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

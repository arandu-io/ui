package main

// The rest of the kit: what makes the nine screens a flow rather than nine
// pages.
//
// It used to publish the sign-in screens and stop there, and the result was a
// kit that looked complete and was not: register.kyse.go and verify.kyse.go
// posted to addresses nobody registered, and the password reset reached the end
// of its flow and logged a line saying it did not write the password.
//
// Everything here is the version the example proves. tests/Feature/Journey_test.go
// in arandu-io/examples walks it: register, confirm, sign in, comment, forget
// the password, reset it, and sign in with the new one while the old one is
// refused.

// authRegisterControllerTemplate is the registration and address-verification handlers.
const authRegisterControllerTemplate = `package authui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	appmail "{{ .ModulePath }}/app/Mail"
)

// Registration and address verification.
//
// # Why the link is signed rather than stored
//
// A verification token is a row in a table in most frameworks, and then there is
// a cleanup job, and a decision about what a click means once the row is gone --
// which is usually "this link is not valid", three months later, to somebody who
// verified correctly the first time.
//
// This one carries the user id and the address it was mailed to
// (auth.VerificationPayload), signed with the application key
// (security.Signer). Nothing is written when the mail goes out, the link says
// "expired" when it has expired rather than "unknown", and a second instance
// behind a load balancer accepts a link the first one issued. The reset link in
// PasswordController is built the same way, and was not: it lived in a map in
// this process, so it worked on whichever replica issued it and a restart threw
// it away.
//
// The purpose is part of the signature, so a verification link is not a password
// reset link even though the same key signed both.
//
// # What verification is worth
//
// It proves control of the address it was sent to, and nothing else. So the
// handler takes the payload of the signed token and lets the service read the
// row: no name, no role and no tenant arrives from the URL, and the service
// refuses the link when the account's address is no longer the one in it.
//
// It is not a login. Clicking the link marks the address verified and sends the
// person to the sign-in screen -- a link in an e-mail that opens an authenticated
// session is a session anybody who reads that inbox, or that forwarded message,
// can have.

// verifyPurpose scopes the signature. Changing this string invalidates every
// link already in an inbox, which is the correct behaviour for a change of
// meaning and a nuisance for a rename -- so it does not get renamed.
const verifyPurpose = "verify-email"

// showRegister draws the sign-up form.
func (m *Module) showRegister(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.register", AuthPage{Page: m.page(r, "Create an account")})
}

// doRegister validates, creates the user and sends the verification link.
func (m *Module) doRegister(w http.ResponseWriter, r *http.Request) {
	in := auth.RegisterRequest{
		Name:                 r.PostFormValue("name"),
		Email:                r.PostFormValue("email"),
		Password:             r.PostFormValue("password"),
		PasswordConfirmation: r.PostFormValue("password_confirmation"),
	}

	// Roles are not read from the form, and RegisterRequest has no field for
	// them. The policy refuses a candidate carrying any, which is the check that
	// still holds if somebody adds the field here.

	u, err := m.auth.Register(r.Context(), m.tenant(r), in)
	if err != nil {
		var invalid validation.Errors
		if errors.As(err, &invalid) {
			m.rejectedRegistration(w, r, in, invalid)
			return
		}
		if errors.Is(err, auth.ErrEmailTaken) {
			// Registration is the one form where "this address is taken" cannot
			// be avoided: the person has to be told why they cannot continue.
			// The sign-in screen is the honest next step, and it is the same
			// answer somebody would get by trying to sign in.
			m.rejectedRegistration(w, r, in, validation.Errors{
				"email": {"that address is already registered. Sign in instead."},
			})
			return
		}
		observability.Log(r.Context()).Error("registration failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	m.sendVerification(r, u)

	// To the notice, not to the inbox. The person is told what to do next on the
	// screen they are already looking at; the mail is the second half.
	redirect(w, r, "/auth/verify")
}

// showVerifyNotice is the "check your inbox" page.
func (m *Module) showVerifyNotice(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.verify", AuthPage{
		Page:                  m.page(r, "Confirm your address"),
		VerificationResendURL: "/auth/verify/resend",
		Resent:                r.URL.Query().Get("resent") == "1",
	})
}

// verify consumes the link.
//
// A GET that changes state, which is the one thing about this flow that is not
// ideal and is not avoidable: what arrives is a click in a mail client, and a
// mail client sends GET. It is safe to repeat -- the second click reports
// "already confirmed" rather than failing -- and it grants nothing, so a prefetch
// that follows it costs the verification and no more.
func (m *Module) verify(w http.ResponseWriter, r *http.Request) {
	payload, err := m.signer.Verify(verifyPurpose, r.URL.Query().Get("token"))
	if err != nil {
		status := "That link is not valid."
		if errors.Is(err, security.ErrExpired) {
			// The one failure a person can act on, so it is the one that gets
			// its own sentence.
			status = "That link has expired. Sign in and we will send another."
		}
		// 422 and not 200. A dead verification link answered 200 with the
		// "check your inbox" page and a message under it, so the browser, the
		// log and every dashboard agreed that a forged token was a confirmed
		// address -- and nothing looked wrong until somebody asked why the
		// verification rate was 100%.
		m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.verify", AuthPage{
			Page:       m.page(r, "Confirm your address"),
			EmailError: status,
		})
		return
	}

	_, first, err := m.auth.MarkVerified(r.Context(), m.tenant(r), payload)
	if err != nil {
		observability.Log(r.Context()).Error("marking an address verified", "error", err)
		m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.verify", AuthPage{
			Page:       m.page(r, "Confirm your address"),
			EmailError: "That link is not valid.",
		})
		return
	}

	// Both answers are a success. Clicking the link a second time -- in another
	// mail client, or because the first tab was closed -- is the common case,
	// and answering it with an error reads as the link being broken.
	message := "Your address is confirmed. Welcome."
	if !first {
		message = "That address was already confirmed. Sign in."
	}
	m.screen(w, r, "auth.login", AuthPage{
		Page:   m.page(r, "Sign in"),
		Status: message,
	})
}

// resendVerification issues another link for the signed-in person.
//
// It requires a session on purpose. A resend form that takes an address is a
// form that mails anybody on request, from this application's domain, as many
// times as it is asked -- and the reputation of a sending domain is not
// something to hand to whoever finds the URL.
func (m *Module) resendVerification(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}

	u, err := m.auth.FindForVerification(r.Context(), subject.Tenant, subject.ID)
	if err != nil {
		observability.Log(r.Context()).Error("reading the user to resend verification", "error", err)
		redirect(w, r, "/auth/verify")
		return
	}
	if u.Verified() {
		redirect(w, r, "/")
		return
	}

	m.sendVerification(r, u)
	redirect(w, r, "/auth/verify?resent=1")
}

// sendVerification signs a link and mails it.
//
// A failure is logged and not returned to the caller: the account exists either
// way, and a registration that answers 500 because a provider was rate limiting
// is a registration the person repeats, which fails on the address being taken.
// The resend button is the recovery, and it is on the next screen.
func (m *Module) sendVerification(r *http.Request, u auth.User) {
	// The id AND the address, so the link stops verifying once the address it
	// was sent to is no longer the account's. Signing the id alone made a link
	// mailed to an old address confirm a new one somebody else controls.
	token := m.signer.Sign(verifyPurpose, auth.VerificationPayload(u), verifyTTL)

	// The origin comes from configuration and never from the Host header: a link
	// built from what the client sent is a link the client chose the destination
	// of, delivered by us, to the address of somebody who trusts us.
	link := m.base + "/auth/verify/confirm?token=" + token

	// The brand travels with the message, from the configuration this module was
	// built with. It is not a literal in the mail view: a starter kit that types
	// its own name there sends every project's verification mail signed with the
	// framework's name, to people who have never heard of it.
	if err := m.mailer.ToAddress(mailAddress(u)).Send(r.Context(), appmail.VerifyEmail{
		Name:      firstWord(u.Name),
		Link:      link,
		BrandName: m.appName,
	}); err != nil {
		observability.Log(r.Context()).Error("sending the verification link",
			"error", err, "user", u)
	}
}

// rejectedRegistration re-renders the form with the messages, per field.
//
// What was typed comes back, except the passwords. Retyping a name and an
// address because one box was wrong is how a sign-up form loses somebody.
func (m *Module) rejectedRegistration(w http.ResponseWriter, r *http.Request, in auth.RegisterRequest, errs validation.Errors) {
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.register", AuthPage{
		Page:  m.page(r, "Create an account"),
		Name:  in.Name,
		Email: in.Email,

		NameError:                 first(errs["name"]),
		EmailError:                first(errs["email"]),
		PasswordError:             first(errs["password"]),
		PasswordConfirmationError: first(errs["password_confirmation"]),
	})
}

func first(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[0]
}

// firstWord is the greeting. "Hello Ada" reads better than "Hello Ada Lovelace",
// and an empty name greets nobody rather than greeting "".
func firstWord(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}
`

// authRenderTemplate is the chrome every screen of the kit shares.
const authRenderTemplate = `package authui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/view"
)

// What every screen of this kit shares: the chrome, the token and the addresses
// the forms post to.
//
// It is in one file because the alternative is each handler building its own
// view.Page, and the moment two of them differ the header on one screen has a
// link the header on the next one does not.

// verifyTTL is how long a verification link works.
//
// A day rather than an hour: the link is read in an inbox, and an inbox is read
// when somebody gets to it. Too short and the common recovery -- "I saw it the
// next morning" -- is a dead link; too long and a forwarded message stays live.
const verifyTTL = 24 * time.Hour

// ChromeProps is what the header needs to know about one request.
//
// A struct and not six parameters: four of them are strings, and a call with
// four positional strings is one somebody eventually writes in the wrong order
// -- the compiler cannot tell a Title from a UserName.
type ChromeProps struct {
	// AppName is the brand in the corner and the name in the title.
	AppName string
	// Title is what this page is.
	Title string
	// Path is the address being read, so the header does not offer a link to
	// the page it is already on.
	Path string
	// Token is this session's CSRF token: the sign-out form carries it, and so
	// does hx-headers on <body>.
	Token string
	// Authenticated and UserName are who is asking. They come from the session
	// and never from the request.
	Authenticated bool
	UserName      string
}

// MarshalJSON keeps this session's CSRF token out of anything that serializes
// the header's props. Without it a single observability.Dump(ctx, "chrome", p)
// publishes it on the debug page, and a token read off a screenshot is a write
// in somebody else's session.
//
// It names the fields that may leave, like AuthPage.MarshalJSON: a field added
// to the struct later does not appear until it is named here. The token is
// named, and carries the marker instead of the value, so a dump still answers
// whether one was issued -- which is the question when a form comes back 419.
func (p ChromeProps) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		AppName       string
		Title         string
		Path          string
		Token         string
		Authenticated bool
		UserName      string
	}{
		AppName:       p.AppName,
		Title:         p.Title,
		Path:          p.Path,
		Token:         redacted(p.Token),
		Authenticated: p.Authenticated,
		UserName:      p.UserName,
	})
}

// LogValue implements slog.LogValuer, so a log line handed the whole props
// records which screen the header was drawn for and nothing else.
func (p ChromeProps) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("path", p.Path),
		slog.String("title", p.Title),
		slog.Bool("authenticated", p.Authenticated),
	)
}

// Chrome fills the header, in one place, for the screens of this kit and for
// the controllers of the application around them.
//
// It is exported because HomeController is not in this package and draws the
// same header. That controller used to build a view.Page literal of its own and
// the two drifted: the landing page greeted a signed-in visitor with the UUID
// out of their session, offered no way to create an account, and hid a password
// reset this kit had already wired. Every one of those is a field somebody
// forgot, and there is no way to forget a field you do not fill in.
func Chrome(p ChromeProps) view.Page {
	return view.Page{
		Title:         p.Title,
		AppName:       p.AppName,
		Path:          p.Path,
		Token:         p.Token,
		Authenticated: p.Authenticated,
		UserName:      p.UserName,

		HomeURL:   "/",
		LoginURL:  "/auth/login",
		LogoutURL: "/auth/logout",
		// The register link is drawn because this kit answers that address:
		// RegisterController is published and Routes mounts it. A link is drawn
		// only for what answers -- one to a route nobody registered is a 404 the
		// layout put there.
		RegisterURL: "/auth/register",
	}
}

// SignedInName turns the id a session carries into something to greet.
//
// One lookup by primary key, and the id is the fallback: a header is not worth a
// 500. A session carries an id and never a name on purpose -- a name kept in a
// session stays wrong after somebody changes theirs -- so the name has to be
// read, and this is where it is read.
//
// It is exported, and it takes what it needs rather than being a method, because
// three callers need exactly this: the screens of this package, the
// HomeController published beside them, and whatever the application draws its
// own header with. Written out three times it is three answers to "what happens
// when the lookup fails", and only one of them would keep the page rendering.
func SignedInName(ctx context.Context, people *auth.Service, tenant, id string) string {
	if id == "" || people == nil {
		return ""
	}
	names, err := people.Names(ctx, tenant, []string{id})
	if err != nil || names[id] == "" {
		return id
	}
	return names[id]
}

// page is the chrome for one of this package's screens.
//
// The title and the path in, everything else from Chrome. The name used to be a
// literal here and a different literal in LoginController_handlers.go, and
// neither was the one in the configuration -- three names for one application,
// on three screens of one kit.
//
// Who is asking is read here, from the session and never from the request. The
// layout draws one half of its navigation or the other from Authenticated, and
// this function left both fields at their zero value: every screen of the kit
// told a signed-in person they were a guest. Two of them are only ever reached
// WITH a session -- /auth/password/confirm sits behind middleware.RequireAuth,
// and the verify notice is where an unverified account is sent -- so the screen
// whose whole job is to ask somebody for their password offered them a "Login"
// button, a "Register" button and no way to sign out.
func (m *Module) page(r *http.Request, title string) view.Page {
	subject, err := m.sessions.Load(r.Context(), r)

	return Chrome(ChromeProps{
		AppName: m.appName,
		Title:   title,
		Path:    r.URL.Path,

		Authenticated: err == nil,
		// The tenant is the session's and not the resolver's: both are this
		// application's rather than the caller's, but only one of them is the
		// tenant this id belongs to. Under a host-based resolver they differ the
		// moment a session is carried to another customer's host, and the lookup
		// would then run in a tenant that never held the account.
		//
		// A guest never reaches the lookup at all: Load leaves the subject empty,
		// and an empty id is answered without a query.
		UserName: SignedInName(r.Context(), m.auth, subject.Tenant, subject.ID),
	})
}

// screen renders one of the kit's pages with a fresh token.
//
// Every screen here needs one: they all post, and a page rendered without a
// token answers 200 and then refuses the submission with 419 -- which reads like
// a broken session rather than a missing field.
func (m *Module) screen(w http.ResponseWriter, r *http.Request, name string, data AuthPage) {
	m.screenStatus(w, r, http.StatusOK, name, data)
}

// screenStatus is screen with the status code spelled out.
//
// A rejected form answers 422 and not 200. HTMX swaps the fragment either way,
// so a 200 leaves the browser, the log and every dashboard agreeing that the
// registration succeeded -- and nothing looks wrong until somebody asks why the
// sign-up success rate is 100%.
func (m *Module) screenStatus(w http.ResponseWriter, r *http.Request, status int, name string, data AuthPage) {
	token, err := m.csrf.Issue(m.sessions.IDFromRequest(r))
	if err != nil {
		observability.Log(r.Context()).Error("issuing csrf token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if data.Page.Title == "" {
		data.Page = m.page(r, "Account")
	}
	// The path is the request's, whatever the caller built: a screen rendered
	// from another handler still has to say which address it is being read at,
	// or the header offers a link to the page it is on.
	data.Page.Path = r.URL.Path
	data.Page.AppName = m.appName
	data.Page.Token = token

	// The addresses every screen of the kit links to or posts to. They are set
	// here rather than per handler so that a screen reached from two places is
	// the same screen both times.
	data.HasPasswordReset = true
	// RegisterURL is not here: it is view.Page's, and Chrome fills it. Setting
	// it in both places is how the header of one screen ends up offering an
	// address the header of the next one does not.
	data.PasswordEmailURL = "/auth/password/email"
	data.PasswordRequestURL = "/auth/password"
	data.PasswordUpdateURL = "/auth/password/update"
	// The address the confirmation form posts to. It was declared, read by
	// auth/passwords/confirm.kyse.go and assigned nowhere, so that screen
	// rendered action="" and posted to itself -- a binding nothing fills is a
	// control that lies, and this one lied on the screen whose whole job is to
	// ask for a password.
	data.PasswordConfirmURL = "/auth/password/confirm"
	// Where a signed-in person is sent, which for this kit is the front page:
	// doLogin redirects there and HomeController answers it. An application that
	// grows a panel of its own changes this line, and welcome.kyse.go follows.
	data.DashboardURL = "/"
	if data.VerificationResendURL == "" {
		data.VerificationResendURL = "/auth/verify/resend"
	}

	if err := view.NewRenderer().Render(r.Context(), w, status, name, data); err != nil {
		observability.Log(r.Context()).Error("rendering "+name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// mailAddress is the user as a recipient, with the display name.
//
// "Ada Lovelace <ada@example.com>" rather than the bare address: a message with
// a name on it is filed as personal correspondence more often than one without,
// and the name is already known by the time this is called.
func mailAddress(u auth.User) mail.Address {
	return mail.Address{Email: u.Email, Name: u.Name}
}
`

// authPasswordControllerTemplate is the three password screens, wired end to end.
const authPasswordControllerTemplate = `package authui

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/modules/auth"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"

	appmail "{{ .ModulePath }}/app/Mail"
)

// The password screens: forgetting one, choosing a new one, and typing the
// current one again before something that matters.
//
// The kit publishes the three and stops there, on purpose: the handlers write
// to your users table, send through your mailer and decide your rules, so they
// are the application's. This is that application's.
//
// # The link is signed, and stored nowhere
//
// This used to be a package-level map of hashed tokens, and it said of itself
// that it was right for one instance and wrong for two. It was worse than that.
// A restart threw away every link in flight; behind a load balancer a link only
// worked on the replica that issued it; every address anybody typed inserted an
// entry that left only when that exact token was presented, so nothing swept
// what nobody clicked; and asking twice left two live links.
//
// What replaced it is a signature and nothing else. The token carries the
// tenant, the account, the address it was mailed to and a fingerprint of the
// password the account had when it was minted (auth.ResetPayload), signed with
// the application key (security.Signer). Nothing is written when the mail goes
// out. The fingerprint is what makes it single use without a row to delete:
// changing the password changes the hash, so the link that was just used and
// every earlier one stop verifying at the same instant.
//
// The purpose is part of the signature, so a verification link is not a reset
// link even though the same key signed both.
//
// # Why the answer is always the same
//
// Asking for a link answers with the same message whether the address is
// registered or not. The alternative is an oracle: a form that says "no such
// account" confirms which addresses exist, one request at a time. The account is
// looked up before anything is sent -- it used to mail whatever was typed, which
// made this endpoint a way to send mail from this application's domain, to an
// address of the caller's choosing, as often as they asked.

// resetPurpose scopes the signature. Changing this string invalidates every link
// already in an inbox, which is the correct behaviour for a change of meaning
// and a nuisance for a rename -- so it does not get renamed.
const resetPurpose = "password-reset"

// resetTTL is how long a link works. Long enough to reach an inbox and read it,
// short enough that a link forwarded by accident has usually expired.
const resetTTL = time.Hour

// linkSent is the one sentence this form answers with.
//
// A constant, and read from here by both branches, because the anti-enumeration
// property is that the two answers are the same bytes -- and two string literals
// a hundred lines apart are two answers that drift the first time one of them is
// reworded.
const linkSent = "If that address is registered, a link is on its way."

// showPasswordRequest draws the "send me a link" form.
func (m *Module) showPasswordRequest(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.email", AuthPage{Page: m.page(r, "Reset your password")})
}

// sendPasswordLink looks the account up, and mails a signed link if there is one.
//
// The lookup is auth.Service.FindForReset, which takes a unit of the throttle's
// budget before it reads the users table. That is the same throttle the sign-in
// screen is behind and not a second counter, keyed apart from it for one reason:
// somebody who has just got their password wrong five times is exactly the
// person who clicks this, and a shared count would refuse them the recovery at
// the one moment they want it.
func (m *Module) sendPasswordLink(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))

	u, err := m.auth.FindForReset(r.Context(), m.tenant(r), email, middleware.KeyByIP(r))

	var locked auth.TooManyAttemptsError
	switch {
	case err == nil:
		m.sendPasswordReset(r, u)

	case errors.Is(err, auth.ErrUserNotFound):
		// Nothing is sent, and the answer below is the one a registered address
		// gets, to the byte. This branch exists to be empty.

	case errors.As(err, &locked):
		// The lockout is decided before the users table is read, so this answer
		// is the same for an address that is registered and one that never was.
		// Saying how long is what stops somebody pressing the button four more
		// times, and each press is a message this application would have sent.
		w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
		m.screenStatus(w, r, http.StatusTooManyRequests, "auth.passwords.email", AuthPage{
			Page:       m.page(r, "Reset your password"),
			Email:      email,
			EmailError: fmt.Sprintf("too many requests, try again in %d seconds", locked.Seconds()),
		})
		return

	default:
		// An outage. Logged, and answered with the same sentence as everything
		// else: a different answer here would say "this address is worth an error
		// message", which is one bit more than the person needs and one bit more
		// than an attacker had.
		observability.Log(r.Context()).Error("looking up the account to reset", "error", err)
	}

	m.screen(w, r, "auth.passwords.email", AuthPage{
		Page:   m.page(r, "Reset your password"),
		Status: linkSent,
	})
}

// sendPasswordReset signs a link for this account and mails it.
//
// A failure is logged and not returned to the caller, and that is the same
// decision sendVerification makes for the opposite-looking reason: telling the
// person that sending failed tells them the address is registered, which is the
// oracle this whole screen is written to avoid.
func (m *Module) sendPasswordReset(r *http.Request, u auth.User) {
	token := m.signer.Sign(resetPurpose, auth.ResetPayload(u), resetTTL)

	// The origin comes from configuration and never from the Host header: a link
	// built from what the client sent is a link the client chose the destination
	// of, delivered by us, to the address of somebody who trusts us.
	link := m.base + "/auth/password/reset?token=" + token

	// Sent, not printed. Which transport carries it is configuration: in
	// development it is the log transport, so the link still ends up somewhere
	// you can read it -- and the code path is the same one production takes,
	// rather than a branch that only runs on a laptop.
	if err := m.mailer.ToAddress(mailAddress(u)).Send(r.Context(), appmail.PasswordReset{
		Name:      firstWord(u.Name),
		Link:      link,
		BrandName: m.appName,
	}); err != nil {
		observability.Log(r.Context()).Error("sending the password reset", "error", err)
	}
}

// showPasswordReset draws the new-password form, carrying the token and the
// address the link was minted for.
//
// The address is filled in here because the form asks for it and marks it
// Required. It was neither filled in nor read, which made the one field somebody
// had to type on a form reached from their own inbox pure decoration.
func (m *Module) showPasswordReset(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	payload, err := m.signer.Verify(resetPurpose, token)
	if err != nil {
		m.askForAnotherLink(w, r, err)
		return
	}

	m.screen(w, r, "auth.passwords.reset", AuthPage{
		Page:       m.page(r, "Choose a new password"),
		ResetToken: token,
		Email:      auth.ResetAddress(payload),
	})
}

// updatePassword checks everything, then changes the password.
//
// The order is the whole point. This used to consume the token first, so a
// password two characters too short burned the link -- and the form it re-drew
// handed back a token that no longer existed, which reads as the application
// losing the reset rather than as a rule about length. Nothing is consumed until
// the input is known to be acceptable.
func (m *Module) updatePassword(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")

	if password != confirmation {
		m.rejectedReset(w, r, token, email, "", "the two passwords do not match")
		return
	}
	// The length rule, here and not in the service: how long a password has to
	// be is a decision about this application, and a framework that made it would
	// be making it for everybody.
	if len([]rune(password)) < security.MinPasswordLen {
		m.rejectedReset(w, r, token, email,
			fmt.Sprintf("must be at least %d characters", security.MinPasswordLen), "")
		return
	}

	payload, err := m.signer.Verify(resetPurpose, token)
	if err != nil {
		m.askForAnotherLink(w, r, err)
		return
	}

	// The tenant is not passed and must not be: it is inside the payload, signed.
	// Resolving it from the host at this point is what let a link minted for one
	// customer change the password of the account with that address at another.
	u, err := m.auth.ResetPassword(r.Context(), payload, email, password)
	if err != nil {
		if errors.Is(err, auth.ErrResetLinkSpent) {
			m.askForAnotherLink(w, r, err)
			return
		}
		observability.Log(r.Context()).Error("writing the new password", "error", err)
		m.rejectedReset(w, r, token, email, "that did not work. Try again in a moment", "")
		return
	}

	// Every other session of that account ends here, and the empty keepID is
	// deliberate: there is no session on this request to keep, and the one that
	// must stop working belongs to whoever forced the reset. A reset that leaves
	// the other sessions signed in leaves that person signed in.
	//
	// A failure is logged and not shown. The password is already changed, and
	// answering with an error would send somebody to reset it again.
	if err := m.sessions.DestroyOthers(r.Context(), auth.SubjectOf(u), ""); err != nil {
		observability.Log(r.Context()).Error("signing the account's other sessions out", "error", err)
	}

	// It does NOT open a session. The link arrives in an inbox: signing somebody
	// in on the strength of it hands the account to whoever else can read that
	// mailbox, or read the message once it has been forwarded -- and having just
	// ended every other session, opening one from a mail link would be the only
	// session left and the least proven. It is the same decision the
	// verification link makes. The cost is one form.
	m.screen(w, r, "auth.login", AuthPage{
		Page:   m.page(r, "Sign in"),
		Email:  u.Email,
		Status: "Your password has been changed. Sign in with it.",
	})
}

// rejectedReset re-renders the new-password form with the token and the address
// still on it.
//
// Both are carried back because neither is what was wrong: a form that drops the
// token on a rejected password is a form that cannot be submitted a second time,
// and the person's only way out is to ask for another link.
func (m *Module) rejectedReset(w http.ResponseWriter, r *http.Request, token, email, passwordError, confirmationError string) {
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.passwords.reset", AuthPage{
		Page:       m.page(r, "Choose a new password"),
		ResetToken: token,
		Email:      email,

		PasswordError:             passwordError,
		PasswordConfirmationError: confirmationError,
	})
}

// askForAnotherLink is the answer to every link that is not going to work.
//
// One answer for five refusals -- forged, expired, already used, minted for
// another address, for an account that is gone -- because the person can do
// exactly one thing about all of them, and because five distinct sentences are
// five facts about somebody's account handed to whoever is holding a link they
// should not have. Only the expiry is named, because that one is a different
// action: it says the link was real.
//
// It draws the "send me a link" screen rather than the form the dead link led
// to, since that form has nothing left to submit.
func (m *Module) askForAnotherLink(w http.ResponseWriter, r *http.Request, err error) {
	status := "That link is not valid any more. Ask for another one."
	if errors.Is(err, security.ErrExpired) {
		status = "That link has expired. Ask for another one."
	}
	// 422 and not 200, on the GET as well as on the POST: HTMX swaps the body of
	// both, so a 200 would leave the browser, the log and every dashboard
	// agreeing that a dead link was a password reset that worked.
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.passwords.email", AuthPage{
		Page:       m.page(r, "Reset your password"),
		EmailError: status,
	})
}

// showPasswordConfirm asks for the password again, before something that matters.
//
// The screen was published from the beginning and could not be reached: nothing
// assigned PasswordConfirmURL, so the form rendered action="" and posted to
// itself, and there was no route and no handler behind it either. Mount
// middleware.RequireConfirmedPassword on your own sensitive routes and this is
// where it sends people.
func (m *Module) showPasswordConfirm(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.confirm", AuthPage{Page: m.page(r, "Confirm your password")})
}

// confirmPassword checks the password of the session that is already open, and
// stamps the session when it is right.
//
// It goes through auth.Service.ConfirmPassword and not Authenticate: that one is
// the sign-in path, it takes an address rather than a subject, and widening it
// would give one call two meanings. The service call is throttled, which is not
// optional here -- this screen sits behind a session, so an unlimited "is this
// the password?" is a password oracle for whoever stole the cookie.
func (m *Module) confirmPassword(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		// The session went away between the form being drawn and it being
		// posted. There is nothing left to confirm on, and the honest next screen
		// is the one that opens a session.
		redirect(w, r, "/auth/login")
		return
	}

	password := r.PostFormValue("password")
	if password == "" {
		// Refused before the service is called, so an empty submit does not spend
		// a unit of the budget that stands between a stolen cookie and the
		// password behind it.
		m.rejectedConfirmation(w, r, http.StatusUnprocessableEntity, "type your password to go on")
		return
	}

	if err := m.auth.ConfirmPassword(r.Context(), subject, password, middleware.KeyByIP(r)); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			m.rejectedConfirmation(w, r, http.StatusUnauthorized, "that is not the password for this account")
			return
		}
		var locked auth.TooManyAttemptsError
		if errors.As(err, &locked) {
			w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
			m.rejectedConfirmation(w, r, http.StatusTooManyRequests,
				fmt.Sprintf("too many attempts, try again in %d seconds", locked.Seconds()))
			return
		}
		observability.Log(r.Context()).Error("confirming a password", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// The stamp, on the session record. It is reported as a failure rather than
	// redirected around, because security.ErrConfirmationNotStored means the
	// backend accepted it and did not keep it -- and redirecting then sends
	// somebody who has just typed their password correctly back to the form that
	// asked for it, forever, with nothing on screen to explain it.
	if err := m.sessions.Confirm(r.Context(), w, r); err != nil {
		observability.Log(r.Context()).Error("recording the password confirmation", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Back to whatever the guard turned them away from, and the front page when
	// there was nothing in particular. The guard is what remembered it -- by the
	// time a password has been typed, the request that was refused is gone.
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
}

// rejectedConfirmation re-renders the confirmation form with the message.
//
// The status is spelled out at every call site because the three refusals are
// three different things -- a missing field, a wrong password, a lockout -- and
// answering 200 for any of them would make the browser, the log and every
// dashboard agree that the password was confirmed.
func (m *Module) rejectedConfirmation(w http.ResponseWriter, r *http.Request, status int, message string) {
	m.screenStatus(w, r, status, "auth.passwords.confirm", AuthPage{
		Page:          m.page(r, "Confirm your password"),
		PasswordError: message,
	})
}
`

// verifyMailableTemplate is the message that carries the confirmation link.
const verifyMailableTemplate = `package mail

import (
	"github.com/arandu-io/framework/mail"
)

// VerifyEmail carries the link that confirms an address.
//
// It is the first message a new account receives, which makes it the one worth
// getting right: a verification mail that lands in spam is a registration that
// never finishes, and the person has no way to tell whether the account exists.
//
// Two things here are about that and not about the code. The subject says what
// to do rather than announcing the product, and the message has a text part --
// an HTML-only message scores worse with every filter that reads them.
type VerifyEmail struct {
	// Name is who the message greets. A first name, or empty for nobody.
	Name string
	// Link is the signed, expiring address that confirms the account.
	Link string
	// BrandName is what the message is signed with. It comes from the
	// configuration, like the name in the page header: an application with two
	// names is one nobody recognises in an inbox.
	//
	// It is a field and not a literal in the view because the view is published
	// by a starter kit. A word typed into the markup here is the kit's word, and
	// it went out on the verification mail of every project that ran the
	// command -- branded with the name of the framework rather than with theirs.
	BrandName string
}

// Envelope is who it is from and what it says it is.
//
// From is left empty on purpose: the Mailer fills in the application's address,
// so the one place it is configured is config/mail.go. A message that names its
// own sender is one that keeps sending from the old domain after a migration.
func (m VerifyEmail) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Confirm your email address",

		// arandu:begin custom
		// The tag is how a provider's dashboard tells these apart from anything
		// else this application sends -- which matters the first time
		// deliverability drops and the question is "of what".
		Tags: []string{"verify-email"},
		// arandu:end custom
	}
}

// Content is what the body is made of.
//
// Both parts render from this struct, so a field that does not exist is a
// compile error at the line of the .kyse.go rather than a blank space in
// somebody's inbox.
func (m VerifyEmail) Content() mail.Content {
	return mail.Content{
		View:     "mail.verify-email",
		TextView: "mail.verify-email-text",
		Data:     m,
	}
}

// Greeting is the name, or a word that works without one.
//
// A method on the mailable rather than a branch in each of the two views: the
// HTML part and the text part greet the same person, and two @if blocks are two
// chances for one of them to say "Hello ,".
func (m VerifyEmail) Greeting() string {
	if m.Name == "" {
		return "there"
	}
	return m.Name
}

// Compile-time proof that this is a mailable.
var _ mail.Mailable = VerifyEmail{}
`

// passwordMailableTemplate is the message that carries the reset link.
const passwordMailableTemplate = `package mail

import (
	"github.com/arandu-io/framework/mail"
)

// PasswordReset is one message this application sends.
//
// An Envelope and a Content, and nothing else. There is no ShouldQueue: sending
// on the queue is a job that calls Send, and having it decided by an interface
// the type implements somewhere else is how a call that sometimes blocks for two
// seconds becomes impossible to reason about from the line you are reading.
type PasswordReset struct {
	// Name is the Name this message carries.
	Name string
	// Link is the Link this message carries.
	Link string
	// BrandName is what the message is signed with, from the configuration and
	// never from a literal in the view. See VerifyEmail.BrandName: the two
	// messages an application sends have to be signed with the same name, and a
	// name typed into the markup is the starter kit's.
	BrandName string
}

// Envelope is who it is from and what it says it is.
//
// From is left empty on purpose: the Mailer fills in the application's address,
// so the one place it is configured is config/mail.go. A message that names its
// own sender is one that keeps sending from the old domain after a migration.
func (m PasswordReset) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Reset your password",

		// arandu:begin custom
		// The tag is how a provider's dashboard tells these apart from anything
		// else this application sends -- which matters the first time deliverability
		// drops and the question is "of what".
		Tags: []string{"password-reset"},
		// arandu:end custom
	}
}

// Content is what the body is made of.
//
// Two views, because a message with no plain-text part is filed as spam more
// often and shows nothing at all in a client that does not render HTML. Both
// render from this struct, so a field that does not exist is a compile error at
// the line of the .kyse.go rather than a blank space in somebody's inbox.
func (m PasswordReset) Content() mail.Content {
	return mail.Content{
		View:     "mail.password-reset",
		TextView: "mail.password-reset-text",
		Data:     m,
	}
}

// Compile-time proof that this is a mailable.
var _ mail.Mailable = PasswordReset{}
`

// verifyMailViewTemplate is a message body, in kyse like every other view.
//
// It is a call to kyse's mailui and not a table written by hand. Written by
// hand, the two messages this kit sends are two designs -- one built from
// mailui, the other 40 lines of <table> with its own greys, its own radius and
// its own idea of a footer -- and an application whose password reset
// does not look like its verification mail is one the reader is right to
// distrust. What a message is made of is decided in one place, and that place is
// the component library.
const verifyMailViewTemplate = `//go:build kyse

package mail

import (
	"github.com/arandu-io/kyse/mailui"

	appmail "<% .ModulePath %>/app/Mail"
)

@go
// The data this message renders from, named rather than declared: it is
// app/Mail/VerifyEmail.go that owns it, because that is where it is filled in.
type VerifyEmailData = appmail.VerifyEmail
@endgo

{{-- The whole message is one call.
     mailui builds the table, the inline styles and the hidden preheader that a
     mail client needs -- Outlook renders with Word, Gmail strips <style>, and an
     external stylesheet is not fetched at all. Writing that by hand per message
     is how two messages from one application stop looking alike.

     The brand is a field and never a word typed here: this file is published
     into your project by a starter kit, and a name in the markup is the kit's
     name going out on your mail.

     Everything inside the block below is yours and survives a republish: the
     wording, the heading and the footer are what this application says, not what
     the kit says. --}}
{{-- arandu:begin custom --}}
{!! mailui.Layout(mailui.LayoutProps{
	Brand:     .BrandName,
	Heading:   "Confirm your email address",
	Preheader: "One click and the account is yours.",
	Body: mailui.Paragraph("Hello " + .Greeting() + ", and welcome. One click and the account is yours -- until then you can read, but not comment.") +
		mailui.Button(mailui.ButtonProps{Label: "Confirm my address", Href: .Link}) +
		mailui.Small("The link works for 24 hours.") +
		mailui.Fallback(.Link),
	Footer: "If you did not create this account, ignore this message. Nothing was activated and the address will not be written to again.",
}) !!}
{{-- arandu:end custom --}}
`

// verifyMailTextTemplate is a message body, in kyse like every other view.
const verifyMailTextTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
// The plain-text part, rendering from the same data as the HTML one.
type VerifyEmailTextData = appmail.VerifyEmail
@endgo

Confirm your email address

{{-- arandu:begin custom --}}
Hello {{ .Greeting() }}, and welcome. One click and the account is yours --
until then you can read, but not comment.

Confirm your address:
{{ .Link }}

The link works for 24 hours.
{{-- arandu:end custom --}}

--
If you did not create this account, ignore this message. Nothing was activated
and the address will not be written to again.
`

// passwordMailViewTemplate is a message body, in kyse like every other view.
//
// Through mailui, like the verification message. This one was the hand-written
// table: same application, same inbox, two sets of greys and two footers. See
// verifyMailViewTemplate.
const passwordMailViewTemplate = `//go:build kyse

package mail

import (
	"github.com/arandu-io/kyse/mailui"

	appmail "<% .ModulePath %>/app/Mail"
)

@go
// The data this message renders from, named rather than declared: it is
// app/Mail/PasswordReset.go that owns it, because that is where it is filled in.
type PasswordResetData = appmail.PasswordReset
@endgo

{{-- One call, the same one the verification message makes. mailui builds the
     table, the inline styles and the hidden preheader a mail client needs.

     Everything inside the block below is yours and survives a republish. --}}
{{-- arandu:begin custom --}}
{!! mailui.Layout(mailui.LayoutProps{
	Brand:     .BrandName,
	Heading:   "Reset your password",
	Preheader: "The link works once, and only for an hour.",
	Body: mailui.Paragraph("Somebody asked to reset the password for this address. If it was not you, nothing has happened and you can ignore this.") +
		mailui.Button(mailui.ButtonProps{Label: "Choose a new password", Href: .Link}) +
		mailui.Small("The link works once and stops working in an hour.") +
		mailui.Fallback(.Link),
	Footer: "You are receiving this because of an action on your account.",
}) !!}
{{-- arandu:end custom --}}
`

// passwordMailTextTemplate is a message body, in kyse like every other view.
const passwordMailTextTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
// The plain-text part, rendering from the same data as the HTML one.
type PasswordResetTextData = appmail.PasswordReset
@endgo

Reset your password

{{-- arandu:begin custom --}}
Somebody asked to reset the password for this address. If it was not you,
nothing has happened and you can ignore this.

Choose a new password:
{{ .Link }}

The link works once and stops working in an hour.
{{-- arandu:end custom --}}

--
You are receiving this because of an action on your account.
`

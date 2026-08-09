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
// This one carries the user id and is signed with the application key
// (security.Signer). Nothing is written when the mail goes out, the link says
// "expired" when it has expired rather than "unknown", and a second instance
// behind a load balancer accepts a link the first one issued -- which the
// in-memory password reset store in PasswordController does not, and says so.
//
// The purpose is part of the signature, so a verification link is not a password
// reset link even though the same key signed both.
//
// # What verification is worth
//
// It proves control of the address and nothing else. So the handler takes the id
// from the signed payload and reads the row itself: no name, no role and no
// tenant arrives from the URL.
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
		m.screen(w, r, "auth.verify", AuthPage{
			Page:       m.page(r, "Confirm your address"),
			EmailError: status,
		})
		return
	}

	_, first, err := m.auth.MarkVerified(r.Context(), m.tenant(r), payload)
	if err != nil {
		observability.Log(r.Context()).Error("marking an address verified", "error", err)
		m.screen(w, r, "auth.verify", AuthPage{
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
	token := m.signer.Sign(verifyPurpose, u.ID, verifyTTL)

	// The origin comes from configuration and never from the Host header: a link
	// built from what the client sent is a link the client chose the destination
	// of, delivered by us, to the address of somebody who trusts us.
	link := m.base + "/auth/verify/confirm?token=" + token

	if err := m.mailer.ToAddress(mailAddress(u)).Send(r.Context(), appmail.VerifyEmail{
		Name: firstWord(u.Name),
		Link: link,
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

// page is the chrome. The title and the path in, everything else fixed.
//
// The name used to be a literal here and a different literal in
// LoginController_handlers.go, and neither was the one in the configuration --
// three names for one application, on three screens of one kit.
func (m *Module) page(r *http.Request, title string) view.Page {
	return view.Page{
		Title:     title,
		AppName:   m.appName,
		Path:      r.URL.Path,
		HomeURL:   "/",
		LoginURL:  "/auth/login",
		LogoutURL: "/auth/logout",
	}
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
	data.RegisterURL = "/auth/register"
	data.PasswordEmailURL = "/auth/password/email"
	data.PasswordRequestURL = "/auth/password"
	data.PasswordUpdateURL = "/auth/password/update"
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

// authPasswordControllerTemplate is the password reset, wired end to end.
const authPasswordControllerTemplate = `package authui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"

	appmail "{{ .ModulePath }}/app/Mail"
)

// The password reset, wired.
//
// The kit publishes the three screens and stops there, on purpose (ADR 0022):
// the handlers write to your users table, send through your mailer and decide
// your rules, so they are the application's. This is that application's.
//
// # What it does and does not promise
//
// The token is random, single-use and expires. It is stored hashed, so the
// store holding it is not a store of live reset links, and it is compared with
// hmac.Equal rather than == -- the comparison is against a secret and the timing
// of a byte-by-byte one leaks its prefix.
//
// The link is sent through the application's mailer. In development that is the
// log transport, so it still ends up somewhere readable -- but the code path is
// the one production takes, rather than a branch that only runs on a laptop.
//
// It is held in memory. That is right for one instance and wrong for two, in
// exactly the way the session store is: behind a load balancer the reset link
// works only on the replica that issued it. Moving it is one type -- the same
// swap docs/11 describes for sessions -- and saying so here is better than a
// store that looks distributed and is not.
//
// # Why the answer is always the same
//
// Asking for a link answers with the same message whether the address is
// registered or not. The alternative is an oracle: a form that says "no such
// account" is a form that confirms which addresses exist, one request at a time.

// resetTTL is how long a link works. Long enough to reach an inbox and read it,
// short enough that a link forwarded by accident has usually expired.
const resetTTL = time.Hour

// resets is the store. See the note above about one instance.
var resets = struct {
	sync.Mutex
	byHash map[string]reset
}{byHash: map[string]reset{}}

type reset struct {
	email   string
	expires time.Time
}

// showPasswordRequest draws the "send me a link" form.
func (m *Module) showPasswordRequest(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.email", AuthPage{})
}

// sendPasswordLink issues a token and mails the link.
func (m *Module) sendPasswordLink(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))

	if email != "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			observability.Log(r.Context()).Error("generating a reset token", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		token := base64.RawURLEncoding.EncodeToString(raw)

		sum := sha256.Sum256([]byte(token))
		resets.Lock()
		resets.byHash[string(sum[:])] = reset{email: email, expires: time.Now().Add(resetTTL)}
		resets.Unlock()

		// Sent, not printed. Which transport carries it is configuration: in
		// development it is the log transport, so the link still ends up
		// somewhere you can read it -- and the code path is the same one
		// production takes, rather than a branch that only runs on a laptop.
		//
		// The failure is logged and not returned. Telling the person that
		// sending failed tells them the address is registered, which is the
		// oracle this whole handler is written to avoid.
		if err := m.mailer.To(email).Send(r.Context(), appmail.PasswordReset{
			Name: email,
			Link: m.base + "/auth/password/reset?token=" + token,
		}); err != nil {
			observability.Log(r.Context()).Error("sending the password reset", "error", err)
		}
	}

	// The same answer either way. A form that says "no such account" is an
	// oracle for which addresses are registered.
	m.screen(w, r, "auth.passwords.email", AuthPage{
		Status: "If that address is registered, a link is on its way.",
	})
}

// showPasswordReset draws the new-password form, carrying the token.
func (m *Module) showPasswordReset(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.reset", AuthPage{
		ResetToken: r.URL.Query().Get("token"),
	})
}

// updatePassword consumes the token and changes the password.
func (m *Module) updatePassword(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")

	if password != confirmation {
		m.screen(w, r, "auth.passwords.reset", AuthPage{
			ResetToken:                token,
			PasswordConfirmationError: "the two passwords do not match",
		})
		return
	}

	email, err := consume(token)
	if err != nil {
		m.screen(w, r, "auth.passwords.reset", AuthPage{
			EmailError: "that link is not valid any more. Ask for another.",
		})
		return
	}

	// The length rule, here and not in the service: how long a password has to
	// be is a decision about this application, and a framework that made it
	// would be making it for everybody.
	if len([]rune(password)) < security.MinPasswordLen {
		m.screen(w, r, "auth.passwords.reset", AuthPage{
			Page:          m.page(r, "Choose a new password"),
			ResetToken:    token,
			PasswordError: fmt.Sprintf("must be at least %d characters", security.MinPasswordLen),
		})
		return
	}

	// The write. It used to be a log line saying this example did not do it --
	// which made the whole flow a demonstration that stops one step short of
	// working, on the screen where stopping short is least acceptable.
	if _, err := m.auth.SetPassword(r.Context(), m.tenant(r), email, password); err != nil {
		observability.Log(r.Context()).Error("writing the new password", "error", err)
		m.screen(w, r, "auth.passwords.reset", AuthPage{
			Page:       m.page(r, "Choose a new password"),
			EmailError: "That did not work. Ask for another link.",
		})
		return
	}

	// Every session of that account is left alone, and that is a decision worth
	// naming: a password reset that does not sign the other sessions out leaves
	// whoever forced the reset still signed in. Doing it needs a session store
	// that can be asked "every session of this subject", which the in-memory one
	// cannot -- see the note at the top of this file about one instance.
	observability.Log(r.Context()).Info("password reset completed", "email", email)

	m.screen(w, r, "auth.login", AuthPage{
		Page:   m.page(r, "Sign in"),
		Status: "Your password has been changed. Sign in with it.",
	})
}

// consume checks a token and removes it, so a link works once.
//
// The stored key is the hash, so what is held is not a live reset link. The
// comparison is hmac.Equal because it is against a secret, and a byte-by-byte
// one leaks the prefix through its timing.
func consume(token string) (string, error) {
	if token == "" {
		return "", errors.New("no token")
	}
	sum := sha256.Sum256([]byte(token))

	resets.Lock()
	defer resets.Unlock()

	for key, entry := range resets.byHash {
		if !hmac.Equal([]byte(key), sum[:]) {
			continue
		}
		delete(resets.byHash, key)
		if time.Now().After(entry.expires) {
			return "", errors.New("expired")
		}
		return entry.email, nil
	}
	return "", errors.New("unknown token")
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
const verifyMailViewTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
// The data this message renders from, named rather than declared: it is
// app/Mail/VerifyEmail.go that owns it, because that is where it is filled in.
type VerifyEmailData = appmail.VerifyEmail
@endgo

<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Confirm your email address</title>
</head>
{{-- Inline styles, and that is not laziness.
     A mail client strips <style> and ignores an external stylesheet, so the
     class names the rest of this application uses reach nothing here. This is
     one of two places in the project where a hex value belongs in the markup.

     The mark at the top is set in type rather than fetched as an image. Every
     mail client blocks remote images until the reader allows them, so a logo
     delivered that way is a broken-image box on the first impression -- which is
     the one message where the first impression is the whole job. --}}
<body style="margin:0;padding:24px;background:#f4f4f5;font-family:system-ui,-apple-system,'Segoe UI',sans-serif;color:#18181b">
	<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;margin:0 auto">
		<tr>
			<td style="padding:0 4px 20px">
				<span style="font-size:15px;font-weight:600;letter-spacing:-0.01em;color:#18181b">Arandu</span>
			</td>
		</tr>
		<tr>
			<td style="background:#fff;border:1px solid #e4e4e7;border-radius:10px;padding:32px">
				<h1 style="margin:0 0 16px;font-size:20px;font-weight:600;letter-spacing:-0.02em">Confirm your email address</h1>

				{{-- arandu:begin custom --}}
				<p style="margin:0 0 16px;font-size:15px;line-height:1.6">
					Hello {{ .Greeting }}, and welcome. One click and the account is
					yours -- until then you can read, but not comment.
				</p>
				<p style="margin:0 0 24px">
					<a href="{{ .Link }}" style="display:inline-block;background:#18181b;color:#fff;text-decoration:none;padding:12px 20px;border-radius:8px;font-size:15px;font-weight:500">Confirm my address</a>
				</p>
				<p style="margin:0;font-size:13px;color:#71717a;line-height:1.6">
					The link works for 24 hours. If the button does not open, copy
					this address:<br>
					<span style="word-break:break-all">{{ .Link }}</span>
				</p>
				{{-- arandu:end custom --}}
			</td>
		</tr>
		<tr>
			<td style="padding:16px 8px;font-size:12px;color:#a1a1aa;line-height:1.6">
				If you did not create this account, ignore this message. Nothing was
				activated and the address will not be written to again.
			</td>
		</tr>
	</table>
</body>
</html>
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
Hello {{ .Greeting }}, and welcome. One click and the account is yours -- until
then you can read, but not comment.

Confirm your address:
{{ .Link }}

The link works for 24 hours.
{{-- arandu:end custom --}}

--
If you did not create this account, ignore this message. Nothing was activated
and the address will not be written to again.
`

// passwordMailViewTemplate is a message body, in kyse like every other view.
const passwordMailViewTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
// The data this message renders from, named rather than declared: it is
// app/Mail/PasswordReset.go that owns it, because that is where it is filled in.
type PasswordResetData = appmail.PasswordReset
@endgo

<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Reset your password</title>
</head>
{{-- Inline styles, and that is not laziness.
     A mail client strips <style> and ignores an external stylesheet, so the
     class names the rest of this application uses reach nothing here. This is
     the one place in the project where a hex value belongs in the markup. --}}
<body style="margin:0;padding:24px;background:#f6f6f6;font-family:system-ui,-apple-system,'Segoe UI',sans-serif;color:#111">
	<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;margin:0 auto">
		<tr>
			<td style="background:#fff;border:1px solid #e5e5e5;border-radius:8px;padding:32px">
				<h1 style="margin:0 0 16px;font-size:20px;font-weight:600">Reset your password</h1>

				{{-- arandu:begin custom --}}
				<p style="margin:0 0 16px;font-size:15px;line-height:1.6">
					Somebody asked to reset the password for this address. If it was
					not you, nothing has happened and you can ignore this.
				</p>
				<p style="margin:0 0 24px">
					<a href="{{ .Link }}" style="display:inline-block;background:#111;color:#fff;text-decoration:none;padding:12px 20px;border-radius:6px;font-size:15px">Choose a new password</a>
				</p>
				<p style="margin:0;font-size:13px;color:#666;line-height:1.6">
					The link works once and stops working in an hour. If the button
					does not open, copy this address:<br>
					<span style="word-break:break-all">{{ .Link }}</span>
				</p>
				{{-- arandu:end custom --}}
			</td>
		</tr>
		<tr>
			<td style="padding:16px 8px;font-size:12px;color:#777">
				You are receiving this because of an action on your account.
			</td>
		</tr>
	</table>
</body>
</html>
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

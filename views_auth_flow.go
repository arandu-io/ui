package main

// authRegisterControllerTemplate owns registration and purpose-bound e-mail
// verification. Codes are stateful and single-use; no GET request mutates an
// account and no signed verification link is published.
const authRegisterControllerTemplate = `package authui

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/onetime"

	appmail "{{ .ModulePath }}/app/Mail"
	models "{{ .ModulePath }}/app/Models"
	services "{{ .ModulePath }}/app/Services"
)

const (
	verifyPurpose   = "verify-email"
	verificationSent = "If that address is registered, a code is on its way."
)

// RegistrationCredential is what the sign-up form asks a new account to prove
// with, and it is this application's decision.
//
// One value decides both what the form draws and what this handler validates,
// which is what keeps the two from drifting apart. A box the screen does not
// draw is not a box this handler requires, and the reverse -- a rule applied to
// an input nobody can see -- rejects every submission while pointing at a field
// that is not on the page.
type RegistrationCredential int

const (
	// PasswordTwice asks for a password and a confirmation, and rejects a pair
	// that differs. It is the zero value, so it is what an unset field means.
	PasswordTwice RegistrationCredential = iota

	// PasswordOnce asks for a password in a single box. The strength rule is
	// unchanged: what goes is the second box, not the requirement.
	PasswordOnce

	// NoPassword asks for no password at all, for an application where an
	// address or a telephone number is the identity and a single-use code is
	// the proof. Nothing is validated here, and Users.Register is left to
	// decide what a new account has to carry.
	NoPassword
)

// asksForPassword reports whether the form draws a password box.
func (c RegistrationCredential) asksForPassword() bool { return c != NoPassword }

// asksForConfirmation reports whether the form draws a second one.
func (c RegistrationCredential) asksForConfirmation() bool { return c == PasswordTwice }

// registrationAsks is what this application's sign-up form asks for.
//
// This line is the whole of the setting: change it and both the form and the
// validation move together, because both read this value.
//
// NoPassword hands the question to Users.Register, and there is one thing that
// implementation must not do with the empty password it is then given: hash it.
// A stored hash of the empty string is a credential anybody can offer. An
// account with no password is one whose password column is EMPTY, which the
// native provider refuses to authenticate whatever is offered against it, and
// the sign-in screen below refuses an empty password before it compares
// anything -- so neither side of that comparison is ever empty. The column is
// this application's to write, and what this setting asks for is a credential
// that is absent rather than one that is the hash of nothing.
const registrationAsks = PasswordTwice

type registrationInput struct {
	Name                 string
	Email                string
	Password             string
	PasswordConfirmation string
}

// LogValue exposes only whether each field was supplied. Registration input
// contains credentials and account PII, so none of its values belong in logs.
func (in registrationInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("name_supplied", in.Name != ""),
		slog.Bool("email_supplied", in.Email != ""),
		slog.Bool("password_supplied", in.Password != ""),
		slog.Bool("password_confirmation_supplied", in.PasswordConfirmation != ""),
	)
}

func (m *Module) showRegister(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.register", AuthPage{
		Page: m.page(r, "Create an account"),
		WithoutPasswordBox: !registrationAsks.asksForPassword(),
		WithoutConfirmationBox: !registrationAsks.asksForConfirmation(),
	})
}

func (m *Module) doRegister(w http.ResponseWriter, r *http.Request) {
	in := registrationInput{
		Name: strings.TrimSpace(r.PostFormValue("name")),
		Email: strings.TrimSpace(r.PostFormValue("email")),
		Password: r.PostFormValue("password"),
		PasswordConfirmation: r.PostFormValue("password_confirmation"),
	}
	// A form that drew no password box did not collect one, so a password in
	// the body arrived from somewhere else. Dropped rather than passed on:
	// what reaches Users.Register is only ever what this form asked for, and a
	// credential cannot be set on a new account through a field this
	// application does not show.
	if !registrationAsks.asksForPassword() {
		in.Password, in.PasswordConfirmation = "", ""
	}

	errs := validation.Errors{}
	if in.Name == "" {
		errs["name"] = []string{"type your name"}
	}
	if in.Email == "" {
		errs["email"] = []string{"type your email address"}
	}
	if registrationAsks.asksForPassword() && len([]rune(in.Password)) < security.MinPasswordLen {
		errs["password"] = []string{"the password is too short"}
	}
	if registrationAsks.asksForConfirmation() && in.Password != in.PasswordConfirmation {
		errs["password_confirmation"] = []string{"the two passwords do not match"}
	}
	if len(errs) != 0 {
		m.rejectedRegistration(w, r, in, errs)
		return
	}

	u, err := m.users.Register(r.Context(), m.tenant(r), in.Name, in.Email, in.Password)
	if err != nil {
		if errors.Is(err, services.ErrEmailTaken) {
			m.rejectedRegistration(w, r, in, validation.Errors{
				"email": {"that address is already registered. Sign in instead."},
			})
			return
		}
		var invalid validation.Errors
		if errors.As(err, &invalid) {
			m.rejectedRegistration(w, r, in, invalid)
			return
		}
		observability.Log(r.Context()).Error("registration failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := m.sendVerification(r, u); err != nil {
		observability.Log(r.Context()).Error("sending the verification code", "error", err)
	}
	redirect(w, r, "/auth/verify")
}

func (m *Module) showVerifyNotice(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.verify", AuthPage{
		Page: m.page(r, "Confirm your address"),
		Email: strings.TrimSpace(r.URL.Query().Get("email")),
	})
}

// verify is deliberately POST-only. The code is bound to purpose, tenant and
// user, and MarkVerified repeats the captured address condition at the write.
func (m *Module) verify(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	code := strings.TrimSpace(r.PostFormValue("email_code"))
	if email == "" || code == "" {
		m.rejectedVerification(w, r, email, "type the code from your email")
		return
	}
	u, err := m.users.Lookup(r.Context(), m.tenant(r), email)
	if err != nil || m.codes.Consume(r.Context(), verifyPurpose, emailCodeSubject(u), code) != nil {
		m.rejectedVerification(w, r, email, "that code is not valid")
		return
	}
	_, firstVerification, err := m.users.MarkVerified(r.Context(), u.TenantID, u.ID, u.Email)
	if err != nil {
		observability.Log(r.Context()).Error("marking an address verified", "error", err)
		m.rejectedVerification(w, r, email, "that code is not valid")
		return
	}
	status := "That address was already confirmed. Sign in."
	if firstVerification {
		status = "Your address is confirmed. Welcome."
	}
	m.screen(w, r, "auth.login", AuthPage{
		Page: m.page(r, "Sign in"), Email: u.Email, Status: status,
	})
}

// resendVerification does not reveal whether the address exists. The native
// CodeStore applies expiry, cooldown, attempt limits and atomic consumption.
func (m *Module) resendVerification(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	if u, err := m.users.Lookup(r.Context(), m.tenant(r), email); err == nil && !u.Verified() {
		if err := m.sendVerification(r, u); err != nil && !errors.Is(err, onetime.ErrCooldown) {
			observability.Log(r.Context()).Error("resending the verification code", "error", err)
		}
	}
	m.screen(w, r, "auth.verify", AuthPage{
		Page: m.page(r, "Confirm your address"), Email: email,
		Status: verificationSent, Resent: true,
	})
}

func (m *Module) sendVerification(r *http.Request, u models.User) error {
	code, err := m.codes.Issue(r.Context(), verifyPurpose, emailCodeSubject(u))
	if err != nil {
		return err
	}
	return m.mailer.ToAddress(mailAddress(u)).Send(r.Context(), appmail.VerifyEmail{
		Name: firstWord(u.Name), Code: code, BrandName: m.appName,
	})
}

func emailCodeSubject(u models.User) string {
	return u.TenantID + "\x00" + u.ID + "\x00" + services.NormalizeEmail(u.Email)
}

func (m *Module) rejectedVerification(w http.ResponseWriter, r *http.Request, email, message string) {
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.verify", AuthPage{
		Page: m.page(r, "Confirm your address"), Email: email, EmailCodeError: message,
	})
}

func (m *Module) rejectedRegistration(w http.ResponseWriter, r *http.Request, in registrationInput, errs validation.Errors) {
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.register", AuthPage{
		Page: m.page(r, "Create an account"), Name: in.Name, Email: in.Email,
		WithoutPasswordBox: !registrationAsks.asksForPassword(),
		WithoutConfirmationBox: !registrationAsks.asksForConfirmation(),
		NameError: first(errs["name"]), EmailError: first(errs["email"]),
		PasswordError: first(errs["password"]),
		PasswordConfirmationError: first(errs["password_confirmation"]),
	})
}

func first(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[0]
}

func firstWord(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}
`

// authRenderTemplate is shared page and rendering infrastructure. Its account
// dependency is an application-owned interface, never a Framework auth module.
const authRenderTemplate = `package authui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/observability"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/view"

	models "{{ .ModulePath }}/app/Models"
)

type ChromeProps struct {
	AppName       string
	Title         string
	Path          string
	Token         string
	Authenticated bool
	UserName      string
}

func (p ChromeProps) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		AppName, Title, Path, Token, UserName string
		Authenticated bool
	}{p.AppName, p.Title, p.Path, redacted(p.Token), p.UserName, p.Authenticated})
}

func (p ChromeProps) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("path", p.Path), slog.String("title", p.Title),
		slog.Bool("authenticated", p.Authenticated),
	)
}

func Chrome(p ChromeProps) view.Page {
	return view.Page{
		Title: p.Title, AppName: p.AppName, Path: p.Path, Token: p.Token,
		Authenticated: p.Authenticated, UserName: p.UserName,
		HomeURL: "/", LoginURL: "/auth/login", LogoutURL: "/auth/logout",
		RegisterURL: "/auth/register",
	}
}

func SignedInName(ctx context.Context, people UserNames, tenant, id string) string {
	if id == "" || people == nil {
		return ""
	}
	names, err := people.PublicNames(ctx, nativeauth.Subject{Tenant: tenant, ID: id}, []string{id})
	if err != nil || names[id] == "" {
		return id
	}
	return names[id]
}

func (m *Module) page(r *http.Request, title string) view.Page {
	subject, err := m.sessions.Load(r.Context(), r)
	return Chrome(ChromeProps{
		AppName: m.appName, Title: title, Path: r.URL.Path,
		Authenticated: err == nil,
		UserName: SignedInName(r.Context(), m.users, subject.Tenant, subject.ID),
	})
}

func (m *Module) screen(w http.ResponseWriter, r *http.Request, name string, data AuthPage) {
	m.screenStatus(w, r, http.StatusOK, name, data)
}

func (m *Module) fragment(w http.ResponseWriter, r *http.Request, status int, screen, part string, data AuthPage) {
	name := screen
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		name = part
	}
	m.screenStatus(w, r, status, name, data)
}

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
	data.Page.Path = r.URL.Path
	data.Page.AppName = m.appName
	data.Page.Token = token
	data.HasPasswordReset = true
	data.DashboardURL = "/"
	data.PasswordRequestURL = "/auth/password"
	data.PasswordEmailURL = "/auth/password/email"
	data.PasswordUpdateURL = "/auth/password/update"
	data.PasswordConfirmURL = "/auth/password/confirm"
	data.VerificationConfirmURL = "/auth/verify/confirm"
	data.VerificationResendURL = "/auth/verify/resend"
	data.TwoFactorChallengeURL = "/auth/two-factor/challenge"
	data.TwoFactorRecoveryURL = "/auth/two-factor/recovery"
	data.TwoFactorSetupURL = "/auth/two-factor/setup"
	data.TwoFactorSetupConfirmURL = "/auth/two-factor/setup/confirm"
	data.TwoFactorDisableURL = "/auth/two-factor/disable"
	data.RecoveryCodesURL = "/auth/two-factor/recovery-codes"
	if err := view.NewRenderer().Render(r.Context(), w, status, name, data); err != nil {
		observability.Log(r.Context()).Error("rendering "+name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func mailAddress(u models.User) mail.Address {
	return mail.Address{Email: u.Email, Name: u.Name}
}
`

// authPasswordControllerTemplate uses a purpose-bound native one-time code.
// The code subject includes the password fingerprint so every older code stops
// working immediately after a successful reset.
const authPasswordControllerTemplate = `package authui

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/observability"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"

	appmail "{{ .ModulePath }}/app/Mail"
	models "{{ .ModulePath }}/app/Models"
	services "{{ .ModulePath }}/app/Services"
)

const (
	resetPurpose = "reset-password"
	codeSent = "If that address is registered, a code is on its way."
)

func (m *Module) showPasswordRequest(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.email", AuthPage{Page: m.page(r, "Reset your password")})
}

func (m *Module) sendPasswordCode(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	if u, err := m.users.Lookup(r.Context(), m.tenant(r), email); err == nil {
		if err := m.sendPasswordReset(r, u); err != nil {
			observability.Log(r.Context()).Error("sending the password reset code", "error", err)
		}
	}
	m.screen(w, r, "auth.passwords.reset", AuthPage{
		Page: m.page(r, "Choose a new password"), Email: email, Status: codeSent,
	})
}

func (m *Module) sendPasswordReset(r *http.Request, u models.User) error {
	code, err := m.codes.Issue(r.Context(), resetPurpose, resetCodeSubject(u))
	if err != nil {
		return err
	}
	return m.mailer.ToAddress(mailAddress(u)).Send(r.Context(), appmail.PasswordReset{
		Name: firstWord(u.Name), Code: code, BrandName: m.appName,
	})
}

func resetCodeSubject(u models.User) string {
	return u.TenantID + "\x00" + u.ID + "\x00" + services.NormalizeEmail(u.Email) + "\x00" + u.PasswordFingerprint()
}

func (m *Module) showPasswordReset(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.reset", AuthPage{
		Page: m.page(r, "Choose a new password"),
		Email: strings.TrimSpace(r.URL.Query().Get("email")),
	})
}

func (m *Module) updatePassword(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	code := strings.TrimSpace(r.PostFormValue("email_code"))
	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")
	if code == "" {
		m.rejectedReset(w, r, email, "type the code from your email", "", "")
		return
	}
	if password != confirmation {
		m.rejectedReset(w, r, email, "", "", "the two passwords do not match")
		return
	}
	if len([]rune(password)) < security.MinPasswordLen {
		m.rejectedReset(w, r, email, "", fmt.Sprintf("must be at least %d characters", security.MinPasswordLen), "")
		return
	}
	u, err := m.users.Lookup(r.Context(), m.tenant(r), email)
	if err != nil || m.codes.Consume(r.Context(), resetPurpose, resetCodeSubject(u), code) != nil {
		m.rejectedReset(w, r, email, "that code is not valid", "", "")
		return
	}
	capturedEmail := u.Email
	capturedPasswordFingerprint := u.PasswordFingerprint()
	u, err = m.users.ResetPassword(r.Context(), u.TenantID, u.ID, capturedEmail, capturedPasswordFingerprint, password)
	if err != nil {
		observability.Log(r.Context()).Error("writing the new password", "error", err)
		m.rejectedReset(w, r, email, "that code is not valid", "", "")
		return
	}
	if err := m.sessions.DestroyOthers(r.Context(), subjectOf(u), ""); err != nil {
		observability.Log(r.Context()).Error("signing the account's other sessions out", "error", err)
	}
	m.screen(w, r, "auth.login", AuthPage{
		Page: m.page(r, "Sign in"), Email: u.Email,
		Status: "Your password has been changed. Sign in with it.",
	})
}

func (m *Module) rejectedReset(w http.ResponseWriter, r *http.Request, email, codeError, passwordError, confirmationError string) {
	m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.passwords.reset", AuthPage{
		Page: m.page(r, "Choose a new password"), Email: email,
		EmailCodeError: codeError, PasswordError: passwordError,
		PasswordConfirmationError: confirmationError,
	})
}

func (m *Module) showPasswordConfirm(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.confirm", AuthPage{Page: m.page(r, "Confirm your password")})
}

func (m *Module) confirmPassword(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}
	password := r.PostFormValue("password")
	if password == "" {
		m.rejectedConfirmation(w, r, http.StatusUnprocessableEntity, "type your password to go on")
		return
	}
	if err := m.users.ConfirmPassword(r.Context(), subject, password, middleware.KeyByIP(r)); err != nil {
		if errors.Is(err, nativeauth.ErrInvalidCredentials) {
			m.rejectedConfirmation(w, r, http.StatusUnauthorized, "that is not the password for this account")
			return
		}
		var locked retryAfterError
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
	if err := m.sessions.Confirm(r.Context(), w, r); err != nil {
		observability.Log(r.Context()).Error("recording the password confirmation", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
}

func (m *Module) rejectedConfirmation(w http.ResponseWriter, r *http.Request, status int, message string) {
	m.screenStatus(w, r, status, "auth.passwords.confirm", AuthPage{
		Page: m.page(r, "Confirm your password"), PasswordError: message,
	})
}
`

// authTwoFactorControllerTemplate keeps a password-authenticated attempt in a
// short signed cookie. It creates a session only after an authenticator or
// recovery code succeeds.
const authTwoFactorControllerTemplate = `package authui

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arandu-io/framework/observability"
	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/otp"
	"github.com/arandu-io/hesape/qr"

	models "{{ .ModulePath }}/app/Models"
)

const (
	pendingPurpose = "two-factor-pending"
	pendingTTL = 5 * time.Minute
)

type pendingSignIn struct {
	Tenant string ` + "`json:\"tenant\"`" + `
	UserID string ` + "`json:\"user_id\"`" + `
	PasswordFingerprint string ` + "`json:\"password_fingerprint\"`" + `
	Remember bool ` + "`json:\"remember\"`" + `
}

// LogValue redacts the fingerprint while retaining enough context to diagnose
// the short-lived handoff. JSON serialization remains the signed protocol and
// deliberately carries the real fingerprint so a password change invalidates it.
func (p pendingSignIn) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("tenant", p.Tenant),
		slog.String("user_id", p.UserID),
		slog.Bool("password_fingerprint_present", p.PasswordFingerprint != ""),
		slog.Bool("remember", p.Remember),
	)
}

func (m *Module) writePending(w http.ResponseWriter, u models.User, remember bool) error {
	payload, err := json.Marshal(pendingSignIn{
		Tenant: u.TenantID, UserID: u.ID,
		PasswordFingerprint: u.PasswordFingerprint(), Remember: remember,
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: pendingPurpose, Value: m.signer.Sign(pendingPurpose, string(payload), pendingTTL),
		Path: "/auth/two-factor", Expires: time.Now().Add(pendingTTL),
		MaxAge: int(pendingTTL / time.Second), HttpOnly: true, Secure: m.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (m *Module) readPending(r *http.Request) (models.User, bool, error) {
	cookie, err := r.Cookie(pendingPurpose)
	if err != nil {
		return models.User{}, false, err
	}
	encoded, err := m.signer.Verify(pendingPurpose, cookie.Value)
	if err != nil {
		return models.User{}, false, err
	}
	var pending pendingSignIn
	if err := json.Unmarshal([]byte(encoded), &pending); err != nil || pending.Tenant == "" || pending.UserID == "" || pending.PasswordFingerprint == "" {
		return models.User{}, false, errors.New("invalid pending sign-in")
	}
	u, err := m.users.FindForAuthentication(r.Context(), pending.Tenant, pending.UserID)
	if err != nil {
		return models.User{}, false, err
	}
	if subtle.ConstantTimeCompare([]byte(u.PasswordFingerprint()), []byte(pending.PasswordFingerprint)) != 1 {
		return models.User{}, false, errors.New("password changed during pending sign-in")
	}
	return u, pending.Remember, nil
}

func (m *Module) clearPending(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: pendingPurpose, Path: "/auth/two-factor", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Module) showTwoFactorChallenge(w http.ResponseWriter, r *http.Request) {
	if _, _, err := m.readPending(r); err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return
	}
	m.screen(w, r, "auth.two-factor.challenge", AuthPage{Page: m.page(r, "Two-factor challenge")})
}

func (m *Module) verifyTwoFactorChallenge(w http.ResponseWriter, r *http.Request) {
	u, remember, err := m.readPending(r)
	if err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return
	}
	code := strings.TrimSpace(r.PostFormValue("authenticator_code"))
	if code == "" {
		m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.challenge", AuthPage{
			Page: m.page(r, "Two-factor challenge"), AuthenticatorCodeError: "that code is not valid",
		})
		return
	}
	if err := m.factors.VerifyAuthenticator(r.Context(), u.TenantID, u.ID, code); err != nil {
		if errors.Is(err, twofactor.ErrInvalidCode) {
			m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.challenge", AuthPage{
				Page: m.page(r, "Two-factor challenge"), AuthenticatorCodeError: "that code is not valid",
			})
			return
		}
		observability.Log(r.Context()).Error("verifying the authenticator code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.finishSignIn(w, r, u, remember)
}

func (m *Module) showRecoveryChallenge(w http.ResponseWriter, r *http.Request) {
	if _, _, err := m.readPending(r); err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return
	}
	m.screen(w, r, "auth.two-factor.recovery", AuthPage{Page: m.page(r, "Use a recovery code")})
}

func (m *Module) verifyRecoveryChallenge(w http.ResponseWriter, r *http.Request) {
	u, remember, err := m.readPending(r)
	if err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return
	}
	code := strings.TrimSpace(r.PostFormValue("recovery_code"))
	if code == "" {
		m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.recovery", AuthPage{
			Page: m.page(r, "Use a recovery code"), RecoveryCodeError: "that recovery code is not valid",
		})
		return
	}
	if err := m.factors.ConsumeRecovery(r.Context(), u.TenantID, u.ID, code); err != nil {
		if errors.Is(err, twofactor.ErrInvalidCode) {
			m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.recovery", AuthPage{
				Page: m.page(r, "Use a recovery code"), RecoveryCodeError: "that recovery code is not valid",
			})
			return
		}
		observability.Log(r.Context()).Error("consuming the recovery code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.finishSignIn(w, r, u, remember)
}

func (m *Module) showTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.two-factor.setup", AuthPage{Page: m.page(r, "Set up two-factor authentication")})
}

func (m *Module) beginTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}
	provisioning, err := m.factors.Begin(r.Context(), subject, m.appName)
	if err != nil {
		observability.Log(r.Context()).Error("starting two-factor setup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	uri, err := provisioning.URI()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	code, err := qr.Encode(uri)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	svg, err := code.SVG(qr.Options{})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.screen(w, r, "auth.two-factor.setup", AuthPage{
		Page: m.page(r, "Set up two-factor authentication"),
		QRCodeSVG: svg, SecretKey: otp.EncodeSecret(provisioning.Secret),
	})
}

func (m *Module) confirmTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}
	code := strings.TrimSpace(r.PostFormValue("authenticator_code"))
	if code == "" {
		m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.setup", AuthPage{
			Page: m.page(r, "Set up two-factor authentication"), AuthenticatorCodeError: "that code is not valid",
		})
		return
	}
	recoveryCodes, err := m.factors.Confirm(r.Context(), subject, code)
	if err != nil {
		if errors.Is(err, twofactor.ErrInvalidCode) {
			m.screenStatus(w, r, http.StatusUnprocessableEntity, "auth.two-factor.setup", AuthPage{
				Page: m.page(r, "Set up two-factor authentication"), AuthenticatorCodeError: "that code is not valid",
			})
			return
		}
		observability.Log(r.Context()).Error("confirming two-factor setup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.showRecoveryCodes(w, r, recoveryCodes)
}

func (m *Module) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}
	if err := m.factors.Disable(r.Context(), subject); err != nil {
		observability.Log(r.Context()).Error("disabling two-factor authentication", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/")
}

func (m *Module) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	subject, err := m.sessions.Load(r.Context(), r)
	if err != nil {
		redirect(w, r, "/auth/login")
		return
	}
	codes, err := m.factors.RegenerateRecoveryCodes(r.Context(), subject)
	if err != nil {
		observability.Log(r.Context()).Error("regenerating recovery codes", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m.showRecoveryCodes(w, r, codes)
}

func (m *Module) showRecoveryCodes(w http.ResponseWriter, r *http.Request, codes []string) {
	m.screen(w, r, "auth.two-factor.recovery-codes", AuthPage{
		Page: m.page(r, "Recovery codes"), RecoveryCodesText: strings.Join(codes, "\n"),
	})
}
`

const verifyMailableTemplate = `package mail

import "github.com/arandu-io/framework/mail"

// VerifyEmail carries a purpose-bound, single-use code.
type VerifyEmail struct {
	Name string
	Code string
	BrandName string
}

func (m VerifyEmail) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Confirm your email address",
		// arandu:begin custom
		Tags: []string{"verify-email"},
		// arandu:end custom
	}
}

func (m VerifyEmail) Content() mail.Content {
	return mail.Content{View: "mail.verify-email", TextView: "mail.verify-email-text", Data: m}
}

func (m VerifyEmail) Greeting() string {
	if m.Name == "" {
		return "there"
	}
	return m.Name
}

var _ mail.Mailable = VerifyEmail{}
`

const passwordMailableTemplate = `package mail

import "github.com/arandu-io/framework/mail"

// PasswordReset carries a purpose-bound, single-use code.
type PasswordReset struct {
	Name string
	Code string
	BrandName string
}

func (m PasswordReset) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Reset your password",
		// arandu:begin custom
		Tags: []string{"password-reset"},
		// arandu:end custom
	}
}

func (m PasswordReset) Content() mail.Content {
	return mail.Content{View: "mail.password-reset", TextView: "mail.password-reset-text", Data: m}
}

var _ mail.Mailable = PasswordReset{}
`

const verifyMailViewTemplate = `//go:build kyse

package mail

import (
	"github.com/arandu-io/kyse/mailui"
	appmail "<% .ModulePath %>/app/Mail"
)

@go
type VerifyEmailData = appmail.VerifyEmail
@endgo

{{-- arandu:begin custom --}}
{!! mailui.Layout(mailui.LayoutProps{
	Brand: .BrandName,
	Heading: "Confirm your email address",
	Preheader: "Use the code to confirm your address.",
	Body: mailui.Paragraph("Hello " + .Greeting() + ", your confirmation code is " + .Code + ".") +
		mailui.Small("The code is single-use and expires shortly."),
	Footer: "If you did not create this account, ignore this message.",
}) !!}
{{-- arandu:end custom --}}
`

const verifyMailTextTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
type VerifyEmailTextData = appmail.VerifyEmail
@endgo

Confirm your email address

{{-- arandu:begin custom --}}
Hello {{ .Greeting() }}. Your confirmation code is:

{{ .Code }}

The code is single-use and expires shortly.
{{-- arandu:end custom --}}
`

const passwordMailViewTemplate = `//go:build kyse

package mail

import (
	"github.com/arandu-io/kyse/mailui"
	appmail "<% .ModulePath %>/app/Mail"
)

@go
type PasswordResetData = appmail.PasswordReset
@endgo

{{-- arandu:begin custom --}}
{!! mailui.Layout(mailui.LayoutProps{
	Brand: .BrandName,
	Heading: "Reset your password",
	Preheader: "Use the code to choose a new password.",
	Body: mailui.Paragraph("Somebody asked to reset your password. Your reset code is " + .Code + ".") +
		mailui.Small("The code is single-use and expires shortly."),
	Footer: "If it was not you, ignore this message.",
}) !!}
{{-- arandu:end custom --}}
`

const passwordMailTextTemplate = `//go:build kyse

package mail

import appmail "<% .ModulePath %>/app/Mail"

@go
type PasswordResetTextData = appmail.PasswordReset
@endgo

Reset your password

{{-- arandu:begin custom --}}
Your reset code is:

{{ .Code }}

The code is single-use and expires shortly.
{{-- arandu:end custom --}}
`

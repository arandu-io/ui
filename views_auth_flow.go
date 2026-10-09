package main

// authRegisterControllerTemplate owns registration and purpose-bound e-mail
// verification. Codes are stateful and single-use; no GET request mutates an
// account and no signed verification link is published.
const authRegisterControllerTemplate = `package authui

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/arandu-io/hesape/hashing"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/onetime"
	"github.com/arandu-io/hesape/validation"

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

// registrationRequest is what the sign-up form sends.
//
// ctx.Bind fills it through the form tags and nothing else: a key the form does
// not declare reaches no field, so a request that carries roles or a tenant sets
// neither. Every value arrives trimmed.
type registrationRequest struct {
	Name                 string ` + "`form:\"name\"`" + `
	Email                string ` + "`form:\"email\"`" + `
	Password             string ` + "`form:\"password\"`" + `
	PasswordConfirmation string ` + "`form:\"password_confirmation\"`" + `
}

// LogValue exposes only whether each field was supplied. Registration input
// contains credentials and account PII, so none of its values belong in logs.
func (in registrationRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("name_supplied", in.Name != ""),
		slog.Bool("email_supplied", in.Email != ""),
		slog.Bool("password_supplied", in.Password != ""),
		slog.Bool("password_confirmation_supplied", in.PasswordConfirmation != ""),
	)
}

// verifyRequest is what the confirmation form sends: the address and the code
// mailed to it.
type verifyRequest struct {
	Email string ` + "`form:\"email\"`" + `
	Code  string ` + "`form:\"email_code\"`" + `
}

// LogValue keeps the address and the code out of a log line: the code is a
// credential until it is spent.
func (in verifyRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("email_supplied", in.Email != ""),
		slog.Bool("code_supplied", in.Code != ""),
	)
}

// resendRequest is what the form asking for another code sends.
type resendRequest struct {
	Email string ` + "`form:\"email\"`" + `
}

// LogValue keeps the address out of a log line.
func (in resendRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("email_supplied", in.Email != ""))
}

func (m *Module) showRegister(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.register", AuthPage{
		Page: m.page(r, "Create an account"),
		WithoutPasswordBox: !registrationAsks.asksForPassword(),
		WithoutConfirmationBox: !registrationAsks.asksForConfirmation(),
	})
}

// doRegister creates the account. A refused form is returned to the router,
// which sends the person back to it with the messages and what was typed.
func (m *Module) doRegister(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	var in registrationRequest
	if err := ctx.Bind(&in); err != nil {
		return err
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
	if registrationAsks.asksForPassword() && len([]rune(in.Password)) < hashing.MinPasswordLen {
		errs["password"] = []string{"the password is too short"}
	}
	if registrationAsks.asksForConfirmation() && in.Password != in.PasswordConfirmation {
		errs["password_confirmation"] = []string{"the two passwords do not match"}
	}
	if errs.Any() {
		return errs
	}

	u, err := m.users.Register(r.Context(), m.tenant(r), in.Name, in.Email, in.Password)
	if err != nil {
		if errors.Is(err, services.ErrEmailTaken) {
			return validation.Errors{"email": {"that address is already registered. Sign in instead."}}
		}
		// The application's own rules answer the same way as the ones above:
		// validation.Errors is returned as it came, and the router draws it.
		var invalid validation.Errors
		if errors.As(err, &invalid) {
			return err
		}
		log.For(r.Context()).Error("registration failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	if err := m.sendVerification(r, u); err != nil {
		log.For(r.Context()).Error("sending the verification code", "error", err)
	}
	redirect(w, r, "/auth/verify")
	return nil
}

func (m *Module) showVerifyNotice(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.verify", AuthPage{
		Page: m.page(r, "Confirm your address"),
		Email: strings.TrimSpace(r.URL.Query().Get("email")),
	})
}

// verify is deliberately POST-only. The code is bound to purpose, tenant and
// user, and MarkVerified repeats the captured address condition at the write.
func (m *Module) verify(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	var in verifyRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Email == "" || in.Code == "" {
		return validation.Errors{"email_code": {"type the code from your email"}}
	}
	u, err := m.users.Lookup(r.Context(), m.tenant(r), in.Email)
	if err != nil || m.codes.Consume(r.Context(), verifyPurpose, emailCodeSubject(u), in.Code) != nil {
		return validation.Errors{"email_code": {"that code is not valid"}}
	}
	_, firstVerification, err := m.users.MarkVerified(r.Context(), u.TenantID, u.ID, u.Email)
	if err != nil {
		log.For(r.Context()).Error("marking an address verified", "error", err)
		return validation.Errors{"email_code": {"that code is not valid"}}
	}
	status := "That address was already confirmed. Sign in."
	if firstVerification {
		status = "Your address is confirmed. Welcome."
	}
	m.notify(w, r, "/auth/login", status, url.Values{"email": {u.Email}})
	return nil
}

// resendVerification does not reveal whether the address exists. The native
// CodeStore applies expiry, cooldown, attempt limits and atomic consumption.
func (m *Module) resendVerification(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	var in resendRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if u, err := m.users.Lookup(r.Context(), m.tenant(r), in.Email); err == nil && !u.Verified() {
		if err := m.sendVerification(r, u); err != nil && !errors.Is(err, onetime.ErrCooldown) {
			log.For(r.Context()).Error("resending the verification code", "error", err)
		}
	}
	m.notify(w, r, "/auth/verify", verificationSent, url.Values{"email": {in.Email}})
	return nil
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
	"net/url"

	"github.com/arandu-io/framework/mail"
	nativeauth "github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/validation"
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

// statusNotice is the key, in the flash a redirect leaves, of the sentence the
// screen it lands on draws as its status line: an address just confirmed, a
// code just sent, a sign-in turned away by the challenge.
const statusNotice = "status"

// notify sends the person to another screen with a sentence for it to draw.
//
// Every screen this kit draws after a form succeeded is reached this way, by a
// redirect rather than by a body answering the post. Two reasons, and either
// would do: a reload of the screen that follows asks for that screen instead of
// posting the form again, and the form on it remembers an address it can be
// sent back to. A rejected form goes back to the Referer, and a screen drawn at
// a POST-only address is one nothing can be sent back to.
//
// old is what the next screen's boxes start with, such as the address the
// person just used; a password field in it is dropped by the flash.
func (m *Module) notify(w http.ResponseWriter, r *http.Request, to, notice string, old url.Values) {
	m.flash.Write(w, map[string][]string{statusNotice: {notice}}, old)
	redirect(w, r, to)
}

// screen renders a screen of the kit.
//
// It is always 200, because nothing here draws a refusal: a rejected form is
// returned to the router, which sends the person back to it. What that attempt
// left in the flash is put on the page here, for every screen at once -- the
// messages by field, which the components ask for through view.Page.FieldError,
// what was typed, which they start from through view.Page.OldOr, and a notice
// another screen left, which becomes the status line.
//
// The token every screen carries is the one CSRFProtect put on the request
// context, issued for this visitor's session or for their guest cookie when
// they have none. No screen issues one.
func (m *Module) screen(w http.ResponseWriter, r *http.Request, name string, data AuthPage) {
	token, _ := hhttp.CSRFTokenFrom(r.Context())
	if data.Page.Title == "" {
		data.Page = m.page(r, "Account")
	}
	state := hhttp.StateFrom(r.Context())
	data.Page.Errors = validation.Errors{}
	for field, messages := range state.Errors {
		if field == statusNotice {
			if data.Status == "" {
				data.Status = first(messages)
			}
			continue
		}
		data.Page.Errors[field] = messages
	}
	data.Page.Old = state.Old
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
	if err := view.NewRenderer().Render(r.Context(), w, http.StatusOK, name, data); err != nil {
		log.For(r.Context()).Error("rendering "+name, "error", err)
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
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/hashing"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/log"
	hmiddleware "github.com/arandu-io/hesape/routing/middleware"
	"github.com/arandu-io/hesape/validation"

	appmail "{{ .ModulePath }}/app/Mail"
	models "{{ .ModulePath }}/app/Models"
	services "{{ .ModulePath }}/app/Services"
)

const (
	resetPurpose = "reset-password"
	codeSent = "If that address is registered, a code is on its way."
)

// passwordEmailRequest is what the form asking for a reset code sends.
type passwordEmailRequest struct {
	Email string ` + "`form:\"email\"`" + `
}

// LogValue keeps the address out of a log line.
func (in passwordEmailRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("email_supplied", in.Email != ""))
}

// passwordUpdateRequest is what the reset form sends: the address, the code
// mailed to it, and the new password twice.
type passwordUpdateRequest struct {
	Email                string ` + "`form:\"email\"`" + `
	Code                 string ` + "`form:\"email_code\"`" + `
	Password             string ` + "`form:\"password\"`" + `
	PasswordConfirmation string ` + "`form:\"password_confirmation\"`" + `
}

// LogValue says which fields arrived and nothing they carried.
func (in passwordUpdateRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("email_supplied", in.Email != ""),
		slog.Bool("code_supplied", in.Code != ""),
		slog.Bool("password_supplied", in.Password != ""),
		slog.Bool("password_confirmation_supplied", in.PasswordConfirmation != ""),
	)
}

// passwordConfirmRequest is what the form asking for the password again sends.
type passwordConfirmRequest struct {
	Password string ` + "`form:\"password\"`" + `
}

// LogValue says whether the password arrived and nothing else.
func (in passwordConfirmRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("password_supplied", in.Password != ""))
}

func (m *Module) showPasswordRequest(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.email", AuthPage{Page: m.page(r, "Reset your password")})
}

func (m *Module) sendPasswordCode(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	var in passwordEmailRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if u, err := m.users.Lookup(r.Context(), m.tenant(r), in.Email); err == nil {
		if err := m.sendPasswordReset(r, u); err != nil {
			log.For(r.Context()).Error("sending the password reset code", "error", err)
		}
	}
	m.notify(w, r, "/auth/password/reset", codeSent, url.Values{"email": {in.Email}})
	return nil
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

// updatePassword writes the new password. A refused form is returned to the
// router, which sends the person back to it; the code is consumed only once
// the password is acceptable, so a rejection never spends it.
func (m *Module) updatePassword(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	var in passwordUpdateRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Code == "" {
		return validation.Errors{"email_code": {"type the code from your email"}}
	}
	if in.Password != in.PasswordConfirmation {
		return validation.Errors{"password_confirmation": {"the two passwords do not match"}}
	}
	if len([]rune(in.Password)) < hashing.MinPasswordLen {
		return validation.Errors{"password": {fmt.Sprintf("must be at least %d characters", hashing.MinPasswordLen)}}
	}
	u, err := m.users.Lookup(r.Context(), m.tenant(r), in.Email)
	if err != nil || m.codes.Consume(r.Context(), resetPurpose, resetCodeSubject(u), in.Code) != nil {
		return validation.Errors{"email_code": {"that code is not valid"}}
	}
	capturedEmail := u.Email
	capturedPasswordFingerprint := u.PasswordFingerprint()
	u, err = m.users.ResetPassword(r.Context(), u.TenantID, u.ID, capturedEmail, capturedPasswordFingerprint, in.Password)
	if err != nil {
		log.For(r.Context()).Error("writing the new password", "error", err)
		return validation.Errors{"email_code": {"that code is not valid"}}
	}
	if err := m.sessions.DestroyOthers(r.Context(), subjectOf(u), ""); err != nil {
		log.For(r.Context()).Error("signing the account's other sessions out", "error", err)
	}
	m.notify(w, r, "/auth/login", "Your password has been changed. Sign in with it.", url.Values{"email": {u.Email}})
	return nil
}

func (m *Module) showPasswordConfirm(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.passwords.confirm", AuthPage{Page: m.page(r, "Confirm your password")})
}

// confirmPassword checks the password of the person RequireAuth let through,
// whose subject the guard put on the request context.
func (m *Module) confirmPassword(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	subject, ok := nativeauth.SubjectFrom(r.Context())
	if !ok {
		redirect(w, r, "/auth/login")
		return nil
	}
	var in passwordConfirmRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Password == "" {
		return validation.Errors{"password": {"type your password to go on"}}
	}
	if err := m.users.ConfirmPassword(r.Context(), subject, in.Password, hmiddleware.KeyByIP(r)); err != nil {
		if errors.Is(err, nativeauth.ErrInvalidCredentials) {
			return validation.Errors{"password": {"that is not the password for this account"}}
		}
		var locked retryAfterError
		if errors.As(err, &locked) {
			w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
			return validation.Errors{
				"password": {fmt.Sprintf("too many attempts, try again in %d seconds", locked.Seconds())},
			}
		}
		log.For(r.Context()).Error("confirming a password", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	if err := m.sessions.Confirm(r.Context(), w, r); err != nil {
		log.For(r.Context()).Error("recording the password confirmation", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	redirect(w, r, m.sessions.TakeIntended(w, r, "/"))
	return nil
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
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	nativeauth "github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/otp"
	"github.com/arandu-io/hesape/qr"
	"github.com/arandu-io/hesape/validation"

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

// authenticatorCodeRequest is what the challenge and the setup confirmation
// send: the six digits the authenticator app shows.
type authenticatorCodeRequest struct {
	Code string ` + "`form:\"authenticator_code\"`" + `
}

// LogValue says whether a code arrived and never which: a code is a credential
// for the thirty seconds it is good for.
func (in authenticatorCodeRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("code_supplied", in.Code != ""))
}

// recoveryCodeRequest is what the recovery challenge sends: one of the codes
// shown once when two-factor authentication was set up.
type recoveryCodeRequest struct {
	Code string ` + "`form:\"recovery_code\"`" + `
}

// LogValue says whether a code arrived and never which: a recovery code signs
// somebody in on its own.
func (in recoveryCodeRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("code_supplied", in.Code != ""))
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

// verifyTwoFactorChallenge finishes a pending sign-in with an authenticator
// code. A wrong code is returned to the router, which sends the person back to
// the challenge with the message; the pending cookie lives on, so they can try
// again until the account's budget says otherwise.
func (m *Module) verifyTwoFactorChallenge(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	u, remember, err := m.readPending(r)
	if err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return nil
	}
	var in authenticatorCodeRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Code == "" {
		return validation.Errors{"authenticator_code": {"that code is not valid"}}
	}
	if err := m.factors.VerifyAuthenticator(r.Context(), u.TenantID, u.ID, in.Code); err != nil {
		if m.challengeLocked(w, r, err) {
			return nil
		}
		if errors.Is(err, twofactor.ErrInvalidCode) {
			return validation.Errors{"authenticator_code": {"that code is not valid"}}
		}
		log.For(r.Context()).Error("verifying the authenticator code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	m.finishSignIn(w, r, u, remember)
	return nil
}

func (m *Module) showRecoveryChallenge(w http.ResponseWriter, r *http.Request) {
	if _, _, err := m.readPending(r); err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return
	}
	m.screen(w, r, "auth.two-factor.recovery", AuthPage{Page: m.page(r, "Use a recovery code")})
}

// verifyRecoveryChallenge finishes a pending sign-in with a recovery code, and
// refuses one the way verifyTwoFactorChallenge refuses an authenticator code.
func (m *Module) verifyRecoveryChallenge(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	u, remember, err := m.readPending(r)
	if err != nil {
		m.clearPending(w)
		redirect(w, r, "/auth/login")
		return nil
	}
	var in recoveryCodeRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Code == "" {
		return validation.Errors{"recovery_code": {"that recovery code is not valid"}}
	}
	if err := m.factors.ConsumeRecovery(r.Context(), u.TenantID, u.ID, in.Code); err != nil {
		if m.challengeLocked(w, r, err) {
			return nil
		}
		if errors.Is(err, twofactor.ErrInvalidCode) {
			return validation.Errors{"recovery_code": {"that recovery code is not valid"}}
		}
		log.For(r.Context()).Error("consuming the recovery code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	m.finishSignIn(w, r, u, remember)
	return nil
}

// challengeLocked ends the pending sign-in when the account has offered too
// many codes to the challenge, and reports whether it did.
//
// It runs before the wrong-code branch because a lock also matches
// twofactor.ErrInvalidCode: answered as a wrong code, it would send the person
// back to the challenge to try again, and every try would be refused. The
// count belongs to the account, so nothing the pending cookie carries can
// succeed until the window passes -- the cookie is cleared, the person is sent
// back to sign in with the reason on the screen, and Retry-After says how long
// the account waits.
func (m *Module) challengeLocked(w http.ResponseWriter, r *http.Request, err error) bool {
	var locked retryAfterError
	if !errors.As(err, &locked) {
		return false
	}
	m.clearPending(w)
	w.Header().Set("Retry-After", strconv.Itoa(locked.Seconds()))
	m.notify(w, r, "/auth/login", lockedMessage(locked.Seconds()), nil)
	return true
}

// lockedMessage is what the sign-in screen says after a lock, in whole minutes
// rounded up.
func lockedMessage(seconds int) string {
	minutes := (seconds + 59) / 60
	if minutes <= 1 {
		return "Too many codes. Sign in again in a minute."
	}
	return fmt.Sprintf("Too many codes. Sign in again in %d minutes.", minutes)
}

func (m *Module) showTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	m.screen(w, r, "auth.two-factor.setup", AuthPage{Page: m.page(r, "Set up two-factor authentication")})
}

// beginTwoFactorSetup starts enrolment for the signed-in person.
//
// The setup, disable and recovery-code routes sit behind RequireAuth and
// RequireConfirmedPassword, and each reads the subject the guard loaded from
// the request context rather than loading the session a second time. A request
// that arrives without one was routed past the guards, and is sent to sign in.
func (m *Module) beginTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	subject, ok := nativeauth.SubjectFrom(r.Context())
	if !ok {
		redirect(w, r, "/auth/login")
		return
	}
	provisioning, err := m.factors.Begin(r.Context(), subject, m.appName)
	if err != nil {
		log.For(r.Context()).Error("starting two-factor setup", "error", err)
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

// confirmTwoFactorSetup proves the first authenticator code and shows the
// recovery codes, once.
//
// A wrong code is returned to the router like every other refusal, and it
// sends the person back to the setup screen with the message. The secret is not
// carried across that redirect -- it would have to travel in a cookie -- so the
// screen offers to start again, and starting again issues a new one.
func (m *Module) confirmTwoFactorSetup(ctx *hhttp.Context) error {
	w, r := ctx.Response, ctx.Request
	subject, ok := nativeauth.SubjectFrom(r.Context())
	if !ok {
		redirect(w, r, "/auth/login")
		return nil
	}
	var in authenticatorCodeRequest
	if err := ctx.Bind(&in); err != nil {
		return err
	}
	if in.Code == "" {
		return validation.Errors{"authenticator_code": {"that code is not valid"}}
	}
	recoveryCodes, err := m.factors.Confirm(r.Context(), subject, in.Code)
	if err != nil {
		if errors.Is(err, twofactor.ErrInvalidCode) {
			return validation.Errors{"authenticator_code": {"that code is not valid"}}
		}
		log.For(r.Context()).Error("confirming two-factor setup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	m.showRecoveryCodes(w, r, recoveryCodes)
	return nil
}

func (m *Module) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	subject, ok := nativeauth.SubjectFrom(r.Context())
	if !ok {
		redirect(w, r, "/auth/login")
		return
	}
	if err := m.factors.Disable(r.Context(), subject); err != nil {
		log.For(r.Context()).Error("disabling two-factor authentication", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/")
}

func (m *Module) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	subject, ok := nativeauth.SubjectFrom(r.Context())
	if !ok {
		redirect(w, r, "/auth/login")
		return
	}
	codes, err := m.factors.RegenerateRecoveryCodes(r.Context(), subject)
	if err != nil {
		log.For(r.Context()).Error("regenerating recovery codes", "error", err)
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

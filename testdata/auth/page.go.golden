package authui

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
// # Where a rejected form's messages are
//
// Not here. A handler that refuses a form returns validation.Errors to the
// router, which sends the person back with the messages and what was typed in
// the flash, and the screen that follows is drawn whole, like any other page.
// The flash lands in the embedded view.Page -- Errors and Old -- and the inputs
// ask for both through the page they are handed, so a message is never a field
// some handler has to remember to copy.
type AuthPage struct {
	view.Page

	// HasPasswordReset moves the "is this route registered" question to the
	// data: an application that did not register that route hides the link
	// rather than linking to a 404.
	HasPasswordReset bool

	// WithoutPasswordBox and WithoutConfirmationBox switch the sign-up form's
	// two password inputs off. They come from the registration handler's own
	// setting and from nothing a request carries.
	//
	// They are here rather than in the view because the handler validates
	// against the same value: a box the screen does not draw is not one the
	// handler requires. Read the other way round, a rule applied to an input
	// nobody can see rejects every submission and points at a field that is not
	// on the page.
	//
	// Stored as the negative, and that is the part worth keeping. This file is
	// replaced on every publish and the registration handler is not, so a
	// publish of the screens alone writes a new sign-up form beside a handler
	// that predates the setting and fills neither field. What that project has
	// to get is the form it had -- and false means the box is drawn, so it
	// does. The positive spelling would have made the same republish quietly
	// stop asking for a password.
	WithoutPasswordBox     bool
	WithoutConfirmationBox bool

	// StatusAsToast draws the status line as a toast instead of the banner
	// above the form.
	//
	// It comes from the handler's own setting and from nothing a request
	// carries, like the two above -- and it is stored as the positive because
	// the banner is what these screens have always drawn: a publish of the
	// screens alone, beside a handler that predates the setting, leaves it
	// false and the project keeps the banner it had.
	//
	// A banner sits above the form and stays; a toast appears at the edge and
	// leaves. Which is right is a question about the application, and this is
	// where it is answered rather than in five views: "we sent you a code" is a
	// sentence somebody reads once, and "that link has expired" is one they
	// need while they retype the address.
	StatusAsToast bool

	// The addresses these screens post to and link to, beyond the navigation
	// view.Page already carries. They come from the router, through the handler.
	DashboardURL             string
	PasswordRequestURL       string
	PasswordEmailURL         string
	PasswordUpdateURL        string
	PasswordConfirmURL       string
	VerificationConfirmURL   string
	VerificationResendURL    string
	TwoFactorChallengeURL    string
	TwoFactorRecoveryURL     string
	TwoFactorSetupURL        string
	TwoFactorSetupConfirmURL string
	TwoFactorDisableURL      string
	RecoveryCodesURL         string

	// Status is the one-shot message a redirect left behind, such as the
	// confirmation that a reset code was sent. Empty means nothing to say.
	Status string

	// Email is the address a link to the screen carried, for the screens that
	// are reached from a message. What was typed on an attempt that was
	// rejected is not here: it is in view.Page.Old, which the inputs start
	// from, and the messages are in view.Page.Errors, which they ask through
	// FieldError. The flash fills both, on every screen, so no handler does.
	Email string

	// Remember is whether the sign-in screen's remember-me box is drawn ticked:
	// the box is markup rather than a component, so the handler reads it out of
	// what the rejected attempt left behind.
	Remember bool

	// Provisioning material is rendered once and deliberately omitted from
	// MarshalJSON and LogValue.
	QRCodeSVG         string
	SecretKey         string
	RecoveryCodesText string
}

// Compile-time proof that these screens fit the layout, and that a component
// can ask them about a field.
var (
	_ view.Layout     = AuthPage{}
	_ components.Page = AuthPage{}
)

// AsksForPassword reports whether the sign-up form draws a password box.
//
// The screen asks this rather than reading the field, so that the field can be
// the negative and the zero value of this struct can be the form that asks.
func (p AuthPage) AsksForPassword() bool { return !p.WithoutPasswordBox }

// AsksForPasswordConfirmation reports whether it draws a second one.
func (p AuthPage) AsksForPasswordConfirmation() bool { return !p.WithoutConfirmationBox }

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

		HasPasswordReset         bool
		DashboardURL             string
		PasswordRequestURL       string
		PasswordEmailURL         string
		PasswordUpdateURL        string
		PasswordConfirmURL       string
		VerificationConfirmURL   string
		VerificationResendURL    string
		TwoFactorChallengeURL    string
		TwoFactorRecoveryURL     string
		TwoFactorSetupURL        string
		TwoFactorSetupConfirmURL string
		TwoFactorDisableURL      string
		RecoveryCodesURL         string

		Status   string
		Email    string
		Remember bool

		// The messages of a rejected attempt, by field. What was typed is
		// left out with the rest of the input: it is the person's, and a
		// dump is read by whoever is debugging.
		Errors map[string][]string
	}{
		Title:         p.Page.Title,
		AppName:       p.Page.AppName,
		Path:          p.Page.Path,
		Authenticated: p.Page.Authenticated,
		UserName:      p.Page.UserName,
		Token:         redacted(p.Page.Token),

		HasPasswordReset:         p.HasPasswordReset,
		DashboardURL:             p.DashboardURL,
		PasswordRequestURL:       p.PasswordRequestURL,
		PasswordEmailURL:         p.PasswordEmailURL,
		PasswordUpdateURL:        p.PasswordUpdateURL,
		PasswordConfirmURL:       p.PasswordConfirmURL,
		VerificationConfirmURL:   p.VerificationConfirmURL,
		VerificationResendURL:    p.VerificationResendURL,
		TwoFactorChallengeURL:    p.TwoFactorChallengeURL,
		TwoFactorRecoveryURL:     p.TwoFactorRecoveryURL,
		TwoFactorSetupURL:        p.TwoFactorSetupURL,
		TwoFactorSetupConfirmURL: p.TwoFactorSetupConfirmURL,
		TwoFactorDisableURL:      p.TwoFactorDisableURL,
		RecoveryCodesURL:         p.RecoveryCodesURL,

		Status:   p.Status,
		Email:    p.Email,
		Remember: p.Remember,

		Errors: p.Page.Errors,
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

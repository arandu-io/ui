package authui

import "github.com/arandu-io/framework/view"

// AuthPage is what every screen of the starter kit renders from.
//
// One struct for the nine screens rather than one per page: they share a layout
// and a shape, and a field a given screen does not use stays at its zero value
// and is never read. That is cheaper than nine structs repeating the same form
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
type AuthPage struct {
	view.Page

	// HasPasswordReset moves the "is this route registered" question to the
	// data: an application that did not register that route hides the link
	// rather than linking to a 404.
	HasPasswordReset bool

	// The addresses these screens post to and link to, beyond the navigation
	// view.Page already carries. They come from the router, through the handler.
	DashboardURL          string
	PasswordRequestURL    string
	PasswordEmailURL      string
	PasswordUpdateURL     string
	PasswordConfirmURL    string
	VerificationResendURL string

	// Status is the one-shot message a redirect left behind, such as the
	// confirmation that a reset link was sent. Empty means nothing to say.
	Status string
	// Resent says a fresh verification link just went out.
	Resent bool

	// Name, Email and Remember are what the person typed on the attempt that
	// was rejected. The password is deliberately absent: it is never sent back.
	Name     string
	Email    string
	Remember bool

	// ResetToken is the one-time token carried by the link in the reset email.
	ResetToken string

	// The validation messages, one field at a time. Empty means the field was
	// accepted -- there is no @error directive to ask a bag on the side.
	NameError                 string
	EmailError                string
	PasswordError             string
	PasswordConfirmationError string
}

// Compile-time proof that these screens fit the layout.
var _ view.Layout = AuthPage{}

// RememberAttribute is the checked attribute of the remember-me box, or nothing.
//
// A conditional attribute has no directive of its own, and inventing one would
// grow the DSL for a single case (RULE 15). What does not fit a directive is
// written in Go, which is here.
func (p AuthPage) RememberAttribute() string {
	if p.Remember {
		return "checked"
	}
	return ""
}

package authui

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

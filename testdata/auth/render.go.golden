package authui

import (
	"context"
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
		// The tenant is the session's and not the resolver's (RULE 14): both are
		// this application's rather than the caller's, but only one of them is the
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

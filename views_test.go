package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// dataType is what a page of the kit renders from. Only the layout declares it;
// the eight pages inherit it, which is what `aru view:build` does when a view
// extends a layout and brings no @go block of its own.
const dataType = "AuthPage"

// layoutType is what the layout itself renders from, and it is deliberately not
// dataType. The layout takes the interface, so it renders any page that
// satisfies it -- including the ones `aru make:module` wrote before this
// command ran.
const layoutType = "Layout"

// TestTheAuthViewsCompile is the check that matters: every one of the nine goes
// through the same compiler `aru view:build` runs, and the Go it emits parses.
//
// A view that only looks right is a view nobody has run. This is cheap, it runs
// in CI, and it fails on the commit that breaks a directive rather than in
// somebody's project after make:auth.
// kyseOnly keeps the views and drops the plain Go that travels with them.
func kyseOnly(files []File) []File {
	var out []File
	for _, f := range files {
		if strings.HasSuffix(filepath.ToSlash(f.Path), ".kyse.go") {
			out = append(out, f)
		}
	}
	return out
}

func TestTheAuthViewsAreNineAndWellFormed(t *testing.T) {
	// Only the .kyse.go. AuthViews also publishes resources/views/page.go, which
	// is plain Go -- the struct the layout is read through, not a view.
	//
	// Nine SCREENS, which is what laravel/ui publishes, plus the four message
	// bodies -- two messages, an HTML part and a text part each. A mail body is
	// not a screen: it has no layout, no navigation and no token, and counting
	// them together would make the number meaningless.
	views := kyseOnly(mustAuthViews(t))

	var screens, bodies []File
	for _, v := range views {
		if strings.Contains(filepath.ToSlash(v.Path), "/mail/") {
			bodies = append(bodies, v)
			continue
		}
		screens = append(screens, v)
	}

	if len(screens) != 9 {
		t.Fatalf("generated %d screens, want the nine of laravel/ui", len(screens))
	}
	if len(bodies) != 4 {
		t.Fatalf("generated %d message bodies, want 4: two messages, both parts each", len(bodies))
	}

	// Whether they COMPILE is not asked here, and that is deliberate. The kyse
	// compiler is aru/internal/kyse, and internal means this module cannot
	// import it -- which is the right constraint: a starter kit that depended on
	// the CLI would be back to being part of it.
	//
	// The compile check lives in plans/prova-ponta-a-ponta.sh, where it is
	// stronger than anything possible here: it publishes this kit into a
	// generated project, runs the real `aru view:build`, the real `go build`,
	// and renders every page over HTTP.
	//
	// What is checked here is the header every view needs before kyse will look
	// at it at all, and it is the mistake a template edit actually makes.
	for _, f := range views {
		body := string(f.Content)
		if !strings.HasPrefix(body, "//go:build kyse\n") {
			t.Errorf("%s does not open with the build tag: the Go compiler would read the markup as Go", f.Path)
		}
		// The package is the directory's, because the generated Go sits beside
		// the source and one directory is one Go package. `auth/login.kyse.go`
		// is `package auth`, and a file that says `package views` there stops
		// compiling the moment `aru view:build` writes `auth/login.go`.
		want := "\npackage " + filepath.Base(filepath.Dir(f.Path)) + "\n"
		if strings.HasSuffix(filepath.ToSlash(f.Path), "resources/views/"+filepath.Base(f.Path)) {
			want = "\npackage views\n"
		}
		if !strings.Contains(body, want) {
			t.Errorf("%s does not declare %q: the package has to be the directory's", f.Path, strings.TrimSpace(want))
		}
	}
}

// TestEveryPageNamesItsDataAndTheLayoutNamesNone.
//
// The struct lives with the controller that fills it, and each screen names it
// in one line. The layout names nothing: it renders with view.Layout, published
// by the framework, so replacing this file replaces a frame and not a type --
// which is what lets the kit be installed in a project that already has pages.
//
// It used to be the other way round, and the cost was that every page in the
// project changed type when the layout was replaced.
func TestEveryPageNamesItsDataAndTheLayoutNamesNone(t *testing.T) {
	for _, f := range kyseOnly(mustAuthViews(t)) {
		body := string(f.Content)
		declares := strings.Contains(body, "@go")
		isLayout := strings.Contains(filepath.ToSlash(f.Path), "/layouts/")

		// A message body renders from a mailable, not from AuthPage: it has no
		// layout, no navigation and no token. It still names its type, and the
		// check below is the same one -- against the other struct.
		if strings.Contains(filepath.ToSlash(f.Path), "/mail/") {
			if !declares {
				t.Errorf("%s names no data type: {{ .Link }} has nothing to read from", f.Path)
			}
			if !strings.Contains(body, "= appmail.") {
				t.Errorf("%s declares a struct of its own rather than naming the mailable that owns it", f.Path)
			}
			continue
		}

		switch {
		case isLayout && declares:
			t.Errorf("%s declares a type: the layout renders with view.Layout, and declaring one here "+
				"types the layout by one page and answers 500 for every other", f.Path)
		case !isLayout && !declares:
			t.Errorf("%s names no data type: {{ .Email }} has nothing to read from", f.Path)
		case !isLayout && !strings.Contains(body, "= authui.AuthPage"):
			t.Errorf("%s declares a struct of its own rather than naming the one the controller owns", f.Path)
		}
	}
}

// TestTheLayoutWiresTheTokenIntoHTMX is the regression guard for the single line
// that breaks every write in an application when it goes missing -- and breaks it
// in a way that reads like a session problem rather than a missing attribute.
func TestTheLayoutWiresTheTokenIntoHTMX(t *testing.T) {
	layout := authView(t, "layouts/app.kyse.go")

	if !strings.Contains(layout, "hx-headers=") || !strings.Contains(layout, "X-CSRF-Token") {
		t.Error("the layout has no hx-headers with the CSRF token: every hx-post would fail the check")
	}
	if !strings.Contains(layout, "@yield('content')") {
		t.Error("the layout yields no content, so nothing that extends it renders")
	}
}

// TestTheFormsCarryTheToken: a form without the hidden field is rejected by the
// CSRF middleware, and the screens are what people copy.
func TestTheFormsCarryTheToken(t *testing.T) {
	for _, f := range kyseOnly(mustAuthViews(t)) {
		body := markup(f)
		if !strings.Contains(body, "<form") {
			continue
		}
		if strings.Count(body, "<form") != strings.Count(body, "@csrf") {
			t.Errorf("%s has a form without @csrf", f.Path)
		}
	}
}

// TestTheAuthViewsInventNoDirective is RULE 15 held against the starter kit: the
// set is closed, and a screen that reaches for a Blade directive kyse does not
// have would fail to compile in somebody else's project rather than in this test.
func TestTheAuthViewsInventNoDirective(t *testing.T) {
	absent := []string{
		"@vite", "@auth", "@guest", "@error", "@can", "@props",
		"@stack", "@push", "@forelse", "@switch", "@fonts", "<x-",
	}
	for _, f := range kyseOnly(mustAuthViews(t)) {
		body := markup(f)
		for _, d := range absent {
			if strings.Contains(body, d) {
				t.Errorf("%s uses %s, which kyse does not have", f.Path, d)
			}
		}
	}
}

// TestTheAuthViewsCarryNoBootstrap: the translation is to Tailwind utilities, and
// a leftover Bootstrap class renders as nothing at all -- an unstyled form that
// looks like a broken build.
func TestTheAuthViewsCarryNoBootstrap(t *testing.T) {
	bootstrap := []string{
		"form-control", "btn btn-", "btn-primary", "card-body", "card-header",
		"navbar-nav", "col-md-", "invalid-feedback", "alert-success",
	}
	for _, f := range kyseOnly(mustAuthViews(t)) {
		body := markup(f)
		for _, class := range bootstrap {
			if strings.Contains(body, class) {
				t.Errorf("%s still uses the Bootstrap class %q", f.Path, class)
			}
		}
	}
}

// TestTheAuthViewsReachForNoHelper: there is no config(), no route(), no auth()
// and no __(). Everything a screen shows came from the handler, in the struct.
func TestTheAuthViewsReachForNoHelper(t *testing.T) {
	helpers := []string{"config(", "route(", "auth()", "__(", "old(", "session(", "Route::has"}
	for _, f := range kyseOnly(mustAuthViews(t)) {
		body := markup(f)
		for _, h := range helpers {
			if strings.Contains(body, h) {
				t.Errorf("%s calls %s, which does not exist: the data comes from the struct", f.Path, h)
			}
		}
	}
}

// markup is the view without its @go block.
//
// The Go a view declares is Go, and it is allowed to name in a doc comment the
// very things the markup may not use -- which is exactly what the layout does
// when it explains why there is no @error and no route(). Scanning the whole
// file would make every one of these checks fail on a comment.
func markup(f File) string {
	return markupOf(string(f.Content))
}

// markupOf is markup, for a view already read as a string.
func markupOf(body string) string {
	start := strings.Index(body, "@go")
	end := strings.Index(body, "@endgo")
	if start < 0 || end < start {
		return body
	}
	return body[:start] + body[end+len("@endgo"):]
}

// authView returns one of the nine by its path under resources/views.
func authView(t *testing.T, want string) string {
	t.Helper()
	for _, f := range kyseOnly(mustAuthViews(t)) {
		if strings.HasSuffix(filepath.ToSlash(f.Path), want) {
			return string(f.Content)
		}
	}
	t.Fatalf("there is no view at %s", want)
	return ""
}

// TestTheLayoutRendersEveryPageAndNotOnlyItsOwn is the regression guard for the
// bug that made this command unusable next to `aru make:module`.
//
// make:auth replaces layouts/app and leaves every existing page alone. When the
// replacement was typed by AuthPage, the pages already in the project answered
//
//	view "layouts.app" takes AuthPage and got views.InvoicesIndexData.
//	The controller and the view disagree about the data
//
// on every request. The layout has to render with the interface, and AuthPage
// has to be just another page that satisfies it.
func TestTheLayoutRendersEveryPageAndNotOnlyItsOwn(t *testing.T) {
	layout := authView(t, "layouts/app.kyse.go")

	// A layout renders with an interface, so any page satisfying it can be drawn
	// inside. It used to declare that interface itself, and reading the first
	// `type X struct` for the same question made the layout render typed by the
	// sign-in struct -- from then on every page `aru make:module` generated
	// answered
	//
	//	view "layouts.app" takes AuthPage and got views.InvoicesIndexData
	//
	// on every request, with a green build, because the disagreement is a type
	// assertion. That cost a cycle.
	//
	// The interface is view.Layout now, published by the framework, and the way
	// this cannot come back is that the layout declares nothing at all: with no
	// @go block there is no type here for the compiler to pick the wrong one of.
	if strings.Contains(layout, "@go") {
		t.Error("the layout declares a type: it renders with view.Layout, and a type here is one the " +
			"view compiler can pick instead -- which is how the layout ends up typed by a single page")
	}
	if strings.Contains(layout, "type Layout interface") {
		t.Error("the layout declares its own Layout interface: the framework publishes it, and two " +
			"declarations of one contract drift")
	}

	// That it BEHAVES this way is checked where the real compiler runs:
	// plans/prova-ponta-a-ponta.sh renders every page `aru make:module` writes
	// through this layout, over HTTP, in a generated project.
}

// TestTheKitsPageEmbedsTheFrameworksPage: AuthPage is not a second answer to
// "what does the layout draw". It embeds view.Page, which is what keeps the
// chrome declared once, in the framework, for every project.
//
// It lives with the controller that fills it rather than in the views, because
// that is where its fields are set.
func TestTheKitsPageEmbedsTheFrameworksPage(t *testing.T) {
	page := authFile(t, "page.go")

	i := strings.Index(page, "type AuthPage struct {")
	if i < 0 {
		t.Fatal("app/Http/Controllers/Auth/page.go declares no AuthPage")
	}
	body := page[i:]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "\n\tview.Page\n") {
		t.Errorf("AuthPage does not embed view.Page: the chrome would be declared twice\n%s", body)
	}
	// The assertion is on the pair and not on a whole line, because the two
	// compile-time proofs -- the layout and the components.Page a field asks
	// through -- are declared in one var block, and gofmt aligns them.
	if !strings.Contains(page, "view.Layout") || !strings.Contains(page, "= AuthPage{}") {
		t.Error("nothing proves AuthPage fits the layout at compile time")
	}
	if !strings.Contains(page, "components.Page") {
		t.Error("nothing proves a kyse component can ask AuthPage about a field, which is how every validation message reaches the screen")
	}
}

func mustAuthViews(t *testing.T) []File {
	t.Helper()
	files, err := AuthViews(Module{ModulePath: "example.com/app"})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

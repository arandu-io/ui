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
	views := kyseOnly(AuthViews())
	if len(views) != 9 {
		t.Fatalf("generated %d views, want the nine of laravel/ui", len(views))
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
		if !strings.Contains(body, "\npackage views\n") {
			t.Errorf("%s has no package clause: it stops compiling the moment a generated file lands beside it", f.Path)
		}
	}
}

// TestOnlyTheLayoutDeclaresTheData: nine copies of one struct is nine places to
// forget a field. The layout declares it and the pages inherit it, which is also
// what makes @extends type-check.
func TestOnlyTheLayoutDeclaresTheData(t *testing.T) {
	for _, f := range kyseOnly(AuthViews()) {
		declares := strings.Contains(string(f.Content), "@go")
		isLayout := strings.Contains(filepath.ToSlash(f.Path), "/layouts/")

		switch {
		case isLayout && !declares:
			t.Errorf("%s declares no data: the pages that extend it have nothing to render from", f.Path)
		case !isLayout && declares:
			t.Errorf("%s declares its own data: it should inherit the layout's", f.Path)
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
	for _, f := range kyseOnly(AuthViews()) {
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
	for _, f := range kyseOnly(AuthViews()) {
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
	for _, f := range kyseOnly(AuthViews()) {
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
	for _, f := range kyseOnly(AuthViews()) {
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
	for _, f := range kyseOnly(AuthViews()) {
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

	// The layout has to declare BOTH: an interface, which is what it renders
	// with, and a struct, which is what it publishes to the pages that extend
	// it. `aru view:build` reads the first `type X interface` for the first
	// question and the first `type X struct` for the second, so a layout that
	// declares only the struct renders with it -- and then every page in the
	// project that is not this one answers
	//
	//	view "layouts.app" takes AuthPage and got views.InvoicesIndexData
	//
	// on every request, with a green build, because the disagreement is a type
	// assertion. That is the defect this test exists for, and it cost a cycle.
	if !strings.Contains(layout, "type "+layoutType+" interface {") {
		t.Errorf("the layout declares no %s interface: it would render with one page's struct and answer 500 for every other page", layoutType)
	}
	if !strings.Contains(layout, "type "+dataType+" struct {") {
		t.Errorf("the layout declares no %s struct: the pages that extend it have nothing to inherit", dataType)
	}
	// And the interface first, because that is the one `RenderType` picks.
	if i, j := strings.Index(layout, "type "+layoutType+" interface {"), strings.Index(layout, "type "+dataType+" struct {"); i >= 0 && j >= 0 && i > j {
		t.Errorf("the struct is declared before the interface; the render type is read as %s", dataType)
	}

	// That it BEHAVES this way is checked where the real compiler runs:
	// plans/prova-ponta-a-ponta.sh renders every page `aru make:module` writes
	// through this layout, over HTTP, in a generated project.
}

// TestTheKitsPageEmbedsTheSkeletonsPage: AuthPage is not a second answer to
// "what does the layout draw". It embeds views.Page like every other page in the
// project, which is what keeps the chrome declared once.
func TestTheKitsPageEmbedsTheSkeletonsPage(t *testing.T) {
	layout := authView(t, "layouts/app.kyse.go")

	i := strings.Index(layout, "type AuthPage struct {")
	if i < 0 {
		t.Fatal("the layout declares no AuthPage")
	}
	body := layout[i:]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "\n\tPage\n") {
		t.Errorf("AuthPage does not embed Page, so it repeats the chrome the layout draws:\n%s", body)
	}

	// And the fields Page carries are not declared a second time here: two
	// AppName fields would not compile, and one shadowing the other silently is
	// worse.
	for _, field := range []string{"AppName string", "Title string", "Token string", "UserName string"} {
		if strings.Contains(body, "\n\t"+field) {
			t.Errorf("AuthPage declares %q, which views.Page already carries", field)
		}
	}
}

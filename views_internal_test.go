package main

import (
	"path/filepath"
	"strings"
	"testing"
)

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
	// Nine SCREENS, plus the four message bodies -- two messages, an HTML part
	// and a text part each. A mail body is not a screen: it has no layout, no
	// navigation and no token, and counting them together would make the number
	// meaningless.
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
		t.Fatalf("generated %d screens, want 9: the layout, home, welcome, and the six auth screens", len(screens))
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

// TestTheAuthViewsInventNoDirective holds the starter kit to kyse's closed
// directive set: a screen that reaches for a directive kyse does not have would
// fail to compile in somebody else's project rather than in this test.
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

// TestTheAuthViewsCarryNoBootstrap: the styling is Tailwind utilities, and a
// leftover Bootstrap class renders as nothing at all -- an unstyled form that
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

// TestNoScreenInterpolatesWhereAnAttributeNameGoes is the position that has no
// escaper at all.
//
// An HTML entity is not decoded in an attribute name, so a value written there
// is read as syntax rather than as text: whatever escaping runs first, the
// browser still sees attributes. Every other position has an answer -- the body
// escapes, an attribute value escapes, a URL is checked -- and this one is
// refused instead.
//
// The kit had exactly one, on the remember-me box, drawn as
//
//	<input type="checkbox" {{ .RememberAttribute() }}>
//
// with a first-party helper answering "checked" or nothing. Nothing could be
// injected through it, and it was still the wrong thing to ship: these screens
// are what a project copies, so the shape teaches the next form to interpolate
// where an attribute name goes -- and there the value is whatever a handler put
// in the struct.
//
// A conditional attribute is written with @if around the attribute, which is how
// required, autofocus and disabled are already drawn.
func TestNoScreenInterpolatesWhereAnAttributeNameGoes(t *testing.T) {
	for _, f := range kyseOnly(mustAuthViews(t)) {
		for _, line := range attributeNameSites(string(f.Content)) {
			t.Errorf("%s:%d interpolates where an attribute name goes: no escaper covers that position, "+
				"and the value is read as syntax. Put the attribute inside @if instead", f.Path, line)
		}
	}
}

// TestTheRememberBoxIsTickedByTheAttributeAndNotByItsValue.
//
// checked is a presence attribute: checked="false" and checked="" both tick the
// box. So the conditional is the attribute itself, and a box drawn as
// checked="{{ .Remember }}" would come back ticked after every rejected sign-in,
// including the ones where the person had left it alone.
func TestTheRememberBoxIsTickedByTheAttributeAndNotByItsValue(t *testing.T) {
	box := elementCarrying(t, markupOf(authView(t, "auth/login.kyse.go")), `name="remember"`)

	if strings.Contains(box, "checked=") {
		t.Errorf("the remember-me box gives checked a value, which ticks it either way:\n%s", box)
	}
	guard, attribute, end := strings.Index(box, "@if(.Remember)"), strings.Index(box, "checked"), strings.Index(box, "@endif")
	if guard < 0 || attribute < 0 || end < 0 || guard > attribute || attribute > end {
		t.Errorf("the remember-me box does not draw checked inside @if(.Remember), so it is ticked for "+
			"everybody or for nobody:\n%s", box)
	}
	if strings.Contains(box, "{{") || strings.Contains(box, "{!!") {
		t.Errorf("the remember-me box interpolates inside the tag:\n%s", box)
	}
}

// TestAttributeNameSitesFindsTheShapeItGuardsAgainst.
//
// A scanner that answers "nothing found" for every input passes on a tree that
// is full of what it is looking for, and reads as a green check either way. The
// first case is the line this kit shipped until the box was rewritten.
func TestAttributeNameSitesFindsTheShapeItGuardsAgainst(t *testing.T) {
	cases := []struct {
		name   string
		markup string
		want   []int
	}{
		{
			name:   "the shape the remember-me box had",
			markup: "<label>\n\t<input class=\"input\" type=\"checkbox\" {{ .RememberAttribute() }}>\n</label>",
			want:   []int{2},
		},
		{
			name:   "an attribute value is a position with an escaper",
			markup: `<a href="{{ .LoginURL }}">Login</a>`,
			want:   nil,
		},
		{
			name:   "so is the body of an element",
			markup: "<p>{{ .BrandName() }}</p>",
			want:   nil,
		},
		{
			name:   "a quoted value holding the other quote does not end early",
			markup: `<body hx-headers='{"X-CSRF-Token": "{{ .CSRFToken() }}"}'>`,
			want:   nil,
		},
		{
			name:   "a directive inside a tag is Go, and its quotes are not markup",
			markup: "<input\n\ttype=\"text\"\n\t@if(.Placeholder != \"\")\n\t\tplaceholder=\"{{ .Placeholder }}\"\n\t@endif\n>",
			want:   nil,
		},
		{
			name:   "markup written inside a comment is not markup",
			markup: "{{-- was <input {{ .X() }}>\n     and is not any more --}}\n<p>{{ .X() }}</p>",
			want:   nil,
		},
		{
			name:   "an interpolation running past one line keeps its position",
			markup: "<div>\n\t{!! components.Field(components.FieldProps{\n\t\tName: \"email\",\n\t}) !!}\n</div>",
			want:   nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := attributeNameSites(c.markup)
			if len(got) != len(c.want) {
				t.Fatalf("found %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("found line %d, want %d", got[i], c.want[i])
				}
			}
		})
	}
}

// elementCarrying returns the tag that holds want, from its < to its >.
func elementCarrying(t *testing.T, source, want string) string {
	t.Helper()

	at := strings.Index(source, want)
	if at < 0 {
		t.Fatalf("no element carries %s", want)
	}
	start := strings.LastIndex(source[:at], "<")
	end := strings.Index(source[at:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("%s is not inside a tag", want)
	}
	return source[start : at+end+1]
}

// attributeNameSites returns the lines of a view where an interpolation falls
// where an attribute name goes: inside a tag, and outside any quoted value.
//
// It is a tokenizer only as far as that question needs, and the view is read the
// way the kyse parser reads it -- a directive takes a whole line, and what is
// inside its parentheses is Go rather than markup, so a quote in a condition
// does not open an attribute value that never closes.
func attributeNameSites(source string) []int {
	const (
		text = iota
		tag
		quoted
	)

	var out []int
	state := text
	var quote byte
	// closing is set while a comment or an interpolation is still open, and
	// carries the delimiter that ends it: both of them run past one line.
	var closing string

	for n, line := range strings.Split(markupOf(source), "\n") {
		at := 0
		switch {
		case closing != "":
			end := strings.Index(line, closing)
			if end < 0 {
				continue
			}
			at = end + len(closing)
			closing = ""
		case strings.HasPrefix(strings.TrimSpace(line), "@"):
			continue
		}

		for ; at < len(line); at++ {
			opener, closer := delimiterAt(line[at:])
			if opener == "" {
				switch state {
				case text:
					if line[at] == '<' && at+1 < len(line) && startsAName(line[at+1]) {
						state = tag
					}
				case tag:
					switch line[at] {
					case '"', '\'':
						state, quote = quoted, line[at]
					case '>':
						state = text
					}
				case quoted:
					if line[at] == quote {
						state = tag
					}
				}
				continue
			}

			// A comment is not a value and lands nowhere: it is skipped like an
			// interpolation so that markup inside it is not read as markup.
			if state == tag && closer != "--}}" {
				out = append(out, n+1)
			}
			at += len(opener)
			if end := strings.Index(line[at:], closer); end >= 0 {
				at += end + len(closer) - 1
				continue
			}
			closing = closer
			break
		}
	}
	return out
}

// delimiterAt names the kyse delimiter opening at the start of s, with the one
// that closes it. Both are empty when s starts with anything else.
func delimiterAt(s string) (opener, closer string) {
	switch {
	case strings.HasPrefix(s, "{{--"):
		return "{{--", "--}}"
	case strings.HasPrefix(s, "{{"):
		return "{{", "}}"
	case strings.HasPrefix(s, "{!!"):
		return "{!!", "!!}"
	}
	return "", ""
}

// startsAName says whether c can follow < in a tag: a letter, the slash of a
// closing tag, or the ! of a doctype.
func startsAName(c byte) bool {
	return c == '/' || c == '!' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
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

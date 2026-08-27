package main

import (
	"bytes"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// update rewrites the golden files: go test . -update
var update = flag.Bool("update", false, "rewrite the golden files")

func authSpec() Module {
	return Module{ModulePath: "example.test/project"}
}

// TestAuthGolden holds the starter kit to the same standard as the generator:
// the same input produces the same bytes, and a change to the templates shows up
// as a diff in review rather than as a surprise in someone's project.
func TestAuthGolden(t *testing.T) {
	files, err := GenerateAuth(authSpec())
	if err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}
	// The count is here so that adding a file is a decision somebody made rather
	// than one that arrived: six controllers and page.go, two mailables, the
	// nine screens and the four message bodies.
	//
	// It was thirteen, and the missing nine were the ones that made the kit a
	// flow: register.kyse.go and verify.kyse.go posted to addresses nobody
	// registered, and the password reset stopped one step short of writing the
	// password.
	if len(files) != 22 {
		t.Fatalf("generated %d files, want 22", len(files))
	}

	for _, f := range files {
		golden := filepath.Join("testdata", "auth", filepath.Base(f.Path)+".golden")
		if *update {
			if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("reading the golden file: %v (run go test . -update)", err)
		}
		if !bytes.Equal(want, f.Content) {
			t.Errorf("%s differs from the golden file", f.Path)
		}
	}
}

// TestTheGeneratedGoParses is the cheap half of the compilation check: it runs
// everywhere, including in CI, where the sibling checkouts do not exist.
func TestTheGeneratedGoParses(t *testing.T) {
	files, err := GenerateAuth(authSpec())
	if err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}
	for _, f := range files {
		// A .kyse.go is a view, not Go: everything below the package clause is
		// markup, which is exactly why the Go parser refuses it.
		if !strings.HasSuffix(f.Path, ".go") || strings.HasSuffix(f.Path, ".kyse.go") {
			continue
		}
		if _, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.AllErrors); err != nil {
			t.Errorf("%s does not parse: %v", f.Path, err)
		}
	}
}

// TestTheGeneratedTemplateIsNotFormatted: a .kyse.go is markup below the package
// clause, and gofmt would refuse it. This is the regression guard for the day
// somebody makes render call format.Source unconditionally.
func TestTheGeneratedTemplateIsNotFormatted(t *testing.T) {
	files, err := GenerateAuth(authSpec())
	if err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}
	for _, f := range files {
		if !strings.HasSuffix(filepath.ToSlash(f.Path), "auth/login.kyse.go") {
			continue
		}
		// A view reaches the disk as written. It is not gofmt'd, because
		// everything below the package clause is markup and gofmt would refuse
		// it -- the build tag is what keeps the Go compiler out.
		if !bytes.Contains(f.Content, []byte("//go:build kyse")) {
			t.Error("the view has no build tag: the Go compiler would try to parse the markup")
		}
		if !bytes.Contains(f.Content, []byte("@extends('layouts.app')")) {
			t.Error("the login view does not extend the layout")
		}
		return
	}
	t.Fatal("auth/login.kyse.go was not generated")
}

// TestTheLoginScreenRotatesTheSession: keeping the pre-login session id is
// session fixation. The framework's own handler does this, and a starter kit that
// people copy from has to do it too -- `aru doctor` checks for the call.
func TestTheLoginScreenRotatesTheSession(t *testing.T) {
	handlers := authFile(t, "LoginController_handlers.go")

	if !strings.Contains(handlers, "sessions.Rotate(") {
		t.Error("the login handler does not rotate the session: this is session fixation")
	}
	if !strings.Contains(handlers, "sessions.Destroy(") {
		t.Error("logout does not destroy the session on the server")
	}
}

// TestTheFailureMessageDoesNotEnumerateAccounts: telling the person which half
// was wrong turns the login endpoint into a list of which emails exist.
func TestTheFailureMessageDoesNotEnumerateAccounts(t *testing.T) {
	handlers := authFile(t, "LoginController_handlers.go")

	for _, leak := range []string{"no such user", "user not found", "unknown email", "wrong password"} {
		if strings.Contains(strings.ToLower(handlers), leak) {
			t.Errorf("the rejection message says %q, which tells an attacker whether the email exists", leak)
		}
	}
	if strings.Count(handlers, `"invalid email or password"`) != 1 {
		t.Error("the two failure paths do not share one message")
	}
}

// TestTheFormCarriesAFreshToken: the fragment that comes back after a rejection
// has to bring a usable CSRF token, or the second attempt fails the check for
// reasons nobody can see from the browser.
func TestTheFormCarriesAFreshToken(t *testing.T) {
	views := authFile(t, "auth/login.kyse.go")

	// The token is issued in render.go now, which is the one place every screen
	// of the kit goes through. It used to be issued in each handler, and the
	// duplication is what let showLogin drift: it built its own view.Page,
	// skipped the wiring, and shipped a sign-in screen with no way to register
	// and no way to recover a password.
	if !strings.Contains(authFile(t, "render.go"), "csrf.Issue(") {
		t.Error("no screen of the kit issues a token")
	}
	for _, handler := range []string{"LoginController_handlers.go", "RegisterController.go", "PasswordController.go"} {
		if strings.Contains(authFile(t, handler), "csrf.Issue(") {
			t.Errorf("%s issues its own token: that is the duplication render.go exists to remove", handler)
		}
	}
	// @csrf is the directive; it compiles to the hidden input with the token.
	if !strings.Contains(views, "@csrf") {
		t.Error("the form has no CSRF field")
	}
	if !strings.Contains(views, `hx-swap="outerHTML"`) || !strings.Contains(views, `hx-target="this"`) {
		t.Error("the form does not replace itself: the swapped-in markup would keep the stale token")
	}
	if strings.Contains(views, "form.Password") {
		t.Error("the password is echoed back into the form")
	}
}

// TestTheScreenDoesNotReachTheDatabase: a handler that imports the data package
// is the shape `aru doctor` rejects, and the starter kit is what people copy.
func TestTheScreenDoesNotReachTheDatabase(t *testing.T) {
	for _, name := range []string{"Auth/LoginController.go", "LoginController_handlers.go", "auth/login.kyse.go"} {
		content := authFile(t, name)
		for _, forbidden := range []string{`"database/sql"`, "framework/data"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s imports %s: the screen talks to the service, never to the database", name, forbidden)
			}
		}
	}
}

// TestTheTenantDoesNotComeFromTheRequestBody covers the one place where the
// tenant legitimately does not come from a Grant -- login, where there is no
// session yet. It has to come from the resolver the application wired, and
// never from anything the caller sent.
func TestTheTenantDoesNotComeFromTheRequestBody(t *testing.T) {
	handlers := authFile(t, "LoginController_handlers.go")

	if !strings.Contains(handlers, "m.tenant(r)") {
		t.Error("the tenant does not come from the resolver")
	}
	for _, bad := range []string{`PostFormValue("tenant")`, `Header.Get("X-Tenant`, `Query().Get("tenant")`} {
		if strings.Contains(handlers, bad) {
			t.Errorf("the tenant is read from the request (%s): anyone could pick which tenant to authenticate against", bad)
		}
	}
}

// TestTheStarterKitDoesNotMigrate: the users table belongs to the framework's
// auth module. Two modules migrating one table is how a schema ends up with two
// owners and a rollout that deadlocks.
func TestTheStarterKitDoesNotMigrate(t *testing.T) {
	if strings.Contains(authFile(t, "Auth/LoginController.go"), "Migrations()") {
		t.Error("the starter kit declares migrations: the users table already has an owner")
	}
	// The starter kit stopped being a module and became controllers in the
	// project's own tree, so there is no manifest to declare migrations = false.
	// What that declaration protected is unchanged and now structural: it emits
	// no migration at all.
	for _, f := range mustGenerateAuth(t) {
		if strings.Contains(filepath.ToSlash(f.Path), "database/migrations") {
			t.Errorf("the starter kit emitted a migration: %s", f.Path)
		}
	}
}

// TestTheKitComposesTheFrameworksAuthModule is the other half of the rule above,
// and the half that was missing.
//
// The kit declaring no migration is right. The kit being registered *in place
// of* the framework's auth module while declaring no migration was not: the
// wiring the command prints takes auth.New out of the list, so the only module
// that owned the users table left with it. `aru migrate` then applied outbox,
// dead_letter and jobs and no users -- no table, no seeded administrator, no
// login, which is the entire point of the kit.
//
// The fix is composition: the generated Module embeds the framework's, so one
// registration keeps one owner for the table and one handler for the path. This
// test reads the declaration rather than the prose, because prose does not
// migrate anything.
func TestTheKitComposesTheFrameworksAuthModule(t *testing.T) {
	source := authFile(t, "Auth/LoginController.go")

	file, err := parser.ParseFile(token.NewFileSet(), "LoginController.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated controller does not parse: %v", err)
	}

	declared := typeSpec(t, file, "Module")
	structType, ok := declared.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("Module is not a struct: %T", declared.Type)
	}

	var embedded []string
	for _, field := range structType.Fields.List {
		if len(field.Names) > 0 {
			continue
		}
		embedded = append(embedded, types.ExprString(field.Type))
	}
	if !slices.Contains(embedded, "*auth.Module") {
		t.Errorf("the kit's Module does not embed *auth.Module, so registering it in place of auth.New "+
			"leaves the users table with no owner; it embeds %v", embedded)
	}

	// And the delegation is load-bearing, not incidental: the generated file
	// says so at compile time, in the project, the moment somebody drops it.
	if !strings.Contains(source, "kernel.Migratable = (*Module)(nil)") {
		t.Error("the kit does not prove at compile time that it still carries the framework's schema")
	}
}

// TestTheKitBuildsTheModuleItEmbeds: an embedded pointer left nil is a nil
// dereference at the first migration, which is worse than the missing table it
// replaced.
func TestTheKitBuildsTheModuleItEmbeds(t *testing.T) {
	source := authFile(t, "Auth/LoginController.go")

	file, err := parser.ParseFile(token.NewFileSet(), "LoginController.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated controller does not parse: %v", err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "New" {
			continue
		}
		var built bool
		ast.Inspect(fn, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Module" {
				return true
			}
			built = strings.HasPrefix(types.ExprString(kv.Value), "auth.New(")
			return false
		})
		if !built {
			t.Errorf("New does not fill the embedded module with auth.New:\n%s", source)
		}
		return
	}
	t.Fatal("the generated controller declares no New")
}

// typeSpec finds a top-level type declaration by name.
func typeSpec(t *testing.T, file *ast.File, name string) *ast.TypeSpec {
	t.Helper()
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name {
				return ts
			}
		}
	}
	t.Fatalf("the file declares no type %s", name)
	return nil
}

// The nine views land at the nine paths people look for, and the controller
// lands beside them.
//
// The command used to write four files into modules/authui/ and declare itself
// with a manifest. It is not a module any more -- it is the project's own code,
// in the project's own tree, so there is nothing to declare.
func TestTheStarterKitLandsInTheProjectTree(t *testing.T) {
	var paths []string
	for _, f := range mustGenerateAuth(t) {
		paths = append(paths, filepath.ToSlash(f.Path))
	}
	all := strings.Join(paths, "\n")

	for _, want := range []string{
		"app/Http/Controllers/Auth/LoginController.go",
		// The kit owns the controller that renders home, because home renders
		// with the layout's type and the kit replaced the layout.
		"app/Http/Controllers/HomeController.go",
		"resources/views/layouts/app.kyse.go",
		"resources/views/home.kyse.go",
		"resources/views/welcome.kyse.go",
		"resources/views/auth/login.kyse.go",
		"resources/views/auth/register.kyse.go",
		"resources/views/auth/verify.kyse.go",
		"resources/views/auth/passwords/confirm.kyse.go",
		"resources/views/auth/passwords/email.kyse.go",
		"resources/views/auth/passwords/reset.kyse.go",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("%s was not generated", want)
		}
	}

	// And nothing lands in the old tree.
	if strings.Contains(all, "modules/") {
		t.Errorf("the starter kit still writes into modules/:\n%s", all)
	}
}

// TestTheLandingPageDrawsTheSignedInHalf is the regression test for a landing
// page that offered "Login" to somebody who had just signed in.
//
// The layout the kit installs draws two halves of a navigation bar and picks by
// SignedIn(), and it puts the token in hx-headers on <body>. Both come from
// view.Page, and the controller that renders home is the only thing that fills
// them. It filled neither: the login succeeded, the cookie was set, and the page
// still said Login -- with hx-headers carrying an empty token, so the next write
// from that page was refused with 419 for a reason nothing on screen explained.
//
// The three fields are read off the literal rather than grepped for, because
// what matters is that the page hands them over, not that the words appear.
func TestTheLandingPageDrawsTheSignedInHalf(t *testing.T) {
	source := authFile(t, "HomeController.go")

	file, err := parser.ParseFile(token.NewFileSet(), "HomeController.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated controller does not parse: %v", err)
	}

	// Through authui.ChromeProps, which is the header this kit's own screens
	// draw. It used to be a view.Page literal written here, and the two drifted:
	// the one place that fills a header is the only way not to forget a field.
	filled := compositeKeys(t, file, "Index", "authui.ChromeProps")
	for _, want := range []string{"Token", "Authenticated", "UserName"} {
		if _, ok := filled[want]; !ok {
			t.Errorf("the landing page leaves the header's %s at its zero value; it fills %v", want, keysOf(filled))
		}
	}

	// And they come from the session and the issuer, not from a constant: a page
	// that hardcodes Authenticated is a page that is wrong half the time.
	if got := filled["Authenticated"]; got == "true" || got == "false" {
		t.Errorf("Authenticated is the constant %s rather than the state of the session", got)
	}
	// The name is looked up and not taken from the session: a session carries an
	// id, and a page that greets somebody with the UUID out of their own cookie
	// is a page that has never been read by the person it greets.
	if got := filled["UserName"]; got == "subject.ID" {
		t.Error("the landing page greets people with the id in their session rather than with their name")
	}
	for _, want := range []string{"c.sessions.Load(", "c.csrf.Issue("} {
		if !strings.Contains(source, want) {
			t.Errorf("the landing page never calls %s, so it cannot know what it is drawing", want)
		}
	}
}

// TestTheLandingPageIsGivenWhatItReads: the two collaborators arrive through the
// constructor, like every other controller's, rather than being reached for.
func TestTheLandingPageIsGivenWhatItReads(t *testing.T) {
	source := authFile(t, "HomeController.go")

	file, err := parser.ParseFile(token.NewFileSet(), "HomeController.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated controller does not parse: %v", err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "NewHomeController" {
			continue
		}
		var params []string
		for _, field := range fn.Type.Params.List {
			params = append(params, types.ExprString(field.Type))
		}
		for _, want := range []string{"*security.SessionStore", "*security.CSRF"} {
			if !slices.Contains(params, want) {
				t.Errorf("NewHomeController does not take %s, so the page cannot read the session or issue a token; it takes %v", want, params)
			}
		}
		// And the service the id in a session is turned into a name with. Without
		// it the header greets somebody who has just signed in with the UUID out
		// of their own session cookie.
		if !slices.Contains(params, "*auth.Service") {
			t.Errorf("NewHomeController does not take *auth.Service, so the landing page cannot resolve the id in the "+
				"session into a name to greet; it takes %v", params)
		}
		return
	}
	t.Fatal("the generated controller declares no NewHomeController")
}

// constructorNames returns the parameter list of one top-level function of a
// generated file, one entry per parameter.
//
// Per parameter and not per field: `appName, base string` is one field with two
// names, and counting fields is how a check on the number of arguments passes
// while the call it is checking has one too few.
func constructorNames(t *testing.T, source, function string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), function+".go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated file does not parse: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != function {
			continue
		}
		var out []string
		for _, field := range fn.Type.Params.List {
			for range max(len(field.Names), 1) {
				out = append(out, types.ExprString(field.Type))
			}
		}
		return out
	}
	t.Fatalf("the generated file declares no %s", function)
	return nil
}

// callFromWiring is one call out of the instructions the command prints, parsed.
//
// The instructions are Go the person is told to paste into bootstrap/app.go, so
// they are read as Go here rather than matched as text: an argument added to a
// constructor and not to the instruction is a difference of one comma, and
// nobody reviewing a paragraph of prose sees it.
func callFromWiring(t *testing.T, name string) *ast.CallExpr {
	t.Helper()

	at := strings.Index(wiring, name+"(")
	if at < 0 {
		t.Fatalf("the wiring this command prints never mentions %s, so nobody is told to build it", name)
	}

	depth, end := 0, -1
	for i := at + len(name); i < len(wiring) && end < 0; i++ {
		switch wiring[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				end = i + 1
			}
		}
	}
	if end < 0 {
		t.Fatalf("the call to %s in the printed wiring is never closed", name)
	}

	expr, err := parser.ParseExpr(wiring[at:end])
	if err != nil {
		t.Fatalf("the wiring this command prints is not Go: %v\n%s", err, wiring[at:end])
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("%s in the printed wiring is not a call", name)
	}
	return call
}

// TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes.
//
// HomeController.go is in `replaced`: publishing overwrites it with no flag at
// all. So the constructor the kit emits and the call the person is told to write
// in bootstrap/app.go are one fact stated twice, and they shipped disagreeing --
// three parameters emitted, five passed by the project this kit was proved
// against. Running the publisher there broke the build, which is the one thing a
// command whose promise is "run it again" must not do.
//
// The same check covers authui.New, because that instruction has been wrong
// before too.
func TestTheWiringThisCommandPrintsCallsTheConstructorItPublishes(t *testing.T) {
	for _, c := range []struct {
		call, file, constructor string
	}{
		{"controllers.NewHomeController", "HomeController.go", "NewHomeController"},
		{"authui.New", "LoginController.go", "New"},
	} {
		want := constructorNames(t, authFile(t, c.file), c.constructor)
		got := callFromWiring(t, c.call).Args

		if len(got) != len(want) {
			t.Errorf("the printed wiring passes %d arguments to %s and the published constructor takes %d %v: "+
				"somebody following the instruction gets a project that does not compile",
				len(got), c.call, len(want), want)
		}
	}
}

// TestTheProjectsInThisTreeCompileTheConstructorTheKitPublishes.
//
// The drift this catches survived because nobody ran the publisher against the
// projects it publishes into. HomeController.go is replaced without a flag, so
// the moment the kit's constructor stops matching the one a project's
// bootstrap/app.go calls, publishing into that project breaks its build -- and
// nothing in either repository noticed, because each one compiles on its own.
//
// Both siblings are checked: the skeleton, which every new project is a copy of,
// and the showcase, which is where the whole flow is proved end to end. Each is
// skipped when it is not checked out beside this module, because this module is
// released on its own and its CI has only itself.
func TestTheProjectsInThisTreeCompileTheConstructorTheKitPublishes(t *testing.T) {
	want := constructorNames(t, authFile(t, "HomeController.go"), "NewHomeController")

	for _, project := range []string{"arandu", "examples"} {
		t.Run(project, func(t *testing.T) {
			path := filepath.Join("..", project, "app", "Http", "Controllers", "HomeController.go")
			source, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("%s is not checked out beside this module, so nothing here proves the kit still fits it", project)
			}

			got := constructorNames(t, string(source), "NewHomeController")
			if !slices.Equal(got, want) {
				t.Errorf("%s builds its landing page with NewHomeController%v and this kit publishes NewHomeController%v.\n"+
					"Publishing into that project replaces the file and leaves bootstrap/app.go calling a constructor "+
					"that no longer exists: the build breaks, with no flag having been passed.", project, got, want)
			}
		})
	}
}

// TestTheRedirectSurvivesWithoutJavaScript is the regression test for a login
// that answered 200 with an empty body.
//
// The form carries method="post" action="/auth/login" as well as hx-post, and
// that is deliberate: it is the path a browser takes when the scripts have not
// arrived -- both of them are deferred -- or when they never do. HX-Redirect is
// a header only HTMX reads, so a plain form post that succeeded got 200, no
// body, and a blank page.
//
// http.Redirect is where the branch lives: HX-Redirect under HTMX, a 303 with
// a Location otherwise. The handler has to go through it, and the two exits --
// login and logout -- both have to.
func TestTheRedirectSurvivesWithoutJavaScript(t *testing.T) {
	source := authFile(t, "LoginController_handlers.go")

	if strings.Contains(source, `"HX-Redirect"`) {
		t.Error("the handler sets HX-Redirect itself: a form post from a browser without HTMX gets 200 and an empty body")
	}
	if !strings.Contains(source, "http.Redirect(w, r, to)") {
		t.Error("the redirect does not go through http.Redirect, which is what answers 303 to a request that is not HTMX")
	}

	file, err := parser.ParseFile(token.NewFileSet(), "LoginController_handlers.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated handlers do not parse: %v", err)
	}
	// The destination of the sign-in is an expression and not a literal, and
	// TestSigningInLandsOnThePageTheGuardTurnedAwayFrom is what holds that half.
	// Here it is written out so this test still proves what it is named for: that
	// the exit goes through the shared helper whatever it is exiting to.
	for _, handler := range []struct{ name, to string }{
		{"doLogin", `m.sessions.TakeIntended(w, r, "/")`},
		{"doLogout", `"/auth/login"`},
	} {
		if !callsRedirect(t, file, handler.name, handler.to) {
			t.Errorf("%s does not redirect to %s through the shared helper", handler.name, handler.to)
		}
	}
}

// compositeKeys returns the keys of the first composite literal of the given
// type inside the named function, mapped to the expression each is filled with.
func compositeKeys(t *testing.T, file *ast.File, function, literal string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || lit.Type == nil || types.ExprString(lit.Type) != literal {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					out[key.Name] = types.ExprString(kv.Value)
				}
			}
			return false
		})
		return out
	}
	t.Fatalf("the file declares no %s", function)
	return nil
}

// keysOf is what a failure message shows: the fields the page did fill.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// callsRedirect reports whether the named handler calls redirect(w, r, to).
func callsRedirect(t *testing.T, file *ast.File, function, to string) bool {
	t.Helper()

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		var found bool
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "redirect" || len(call.Args) != 3 {
				return true
			}
			found = types.ExprString(call.Args[2]) == to
			return !found
		})
		return found
	}
	t.Fatalf("the file declares no %s", function)
	return false
}

// mustGenerateAuth returns the generated files or fails.
func mustGenerateAuth(t *testing.T) []File {
	t.Helper()
	files, err := GenerateAuth(authSpec())
	if err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}
	return files
}

// TestTheStarterKitIsRegenerable: without the custom markers, regenerating eats
// whatever the project added -- and a generator people are afraid to rerun is a
// one-time scaffold.
func TestTheStarterKitIsRegenerable(t *testing.T) {
	for _, name := range []string{"Auth/LoginController.go", "LoginController_handlers.go"} {
		if !strings.Contains(authFile(t, name), "arandu:begin custom") {
			t.Errorf("%s has no custom block: a regeneration would discard the project's additions", name)
		}
	}
}

func TestAuthNeedsTheModulePath(t *testing.T) {
	if _, err := GenerateAuth(Module{}); err == nil {
		t.Fatal("generated without a module path")
	}
}

func authFile(t *testing.T, name string) string {
	t.Helper()
	files, err := GenerateAuth(authSpec())
	if err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}
	// Matched by suffix, not by exact base name. The tree has moved once and the
	// file names moved with it; a test pinned to one spelling breaks on a rename
	// that changed nothing it was testing.
	for _, f := range files {
		if strings.HasSuffix(filepath.ToSlash(f.Path), name) {
			return string(f.Content)
		}
	}
	t.Fatalf("%s was not generated", name)
	return ""
}

// The versions a project's go.mod names when this kit publishes into it.
//
// They are tags on the proxy and not the checkouts beside this module, which is
// the whole of what the gate below is for: a `replace` pointed at ../framework
// compiles the working tree, and the working tree is the one framework nobody
// receives.
//
// These two are what the skeleton pins, so they are what a project starts life
// with. Bump them together with arandu/go.mod --
// TestTheVersionsThisGateCompilesAgainstAreTheOnesANewProjectGets says so when
// they drift apart.
const (
	publishedFramework = "v0.38.1"
	publishedKyse      = "v0.12.1"
)

// TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework is the gate
// that was missing on the day render.go shipped unbuildable.
//
// SignedInName called auth.Service.Names, which had been renamed to PublicNames
// and given a different signature, and every project that ran `go run
// github.com/arandu-io/ui@latest auth` received a file that does not build.
// Nothing here noticed: this module declares no dependency on the framework, its
// CI has only itself, and the golden files compare bytes against bytes. A second
// defect was hiding behind the first -- the published template carries its own
// import block, so adding a call to security.Subject without adding the import
// failed one line later.
//
// A real `go build`, and not go/parser: a parser accepts a file that names a
// symbol nothing declares, which is the exact shape of both halves. It accepted
// them.
//
// No `replace`, for the reason written above the two versions. The tree is the
// published tree and not a flattened copy of it either: RegisterController
// imports the project's own app/Mail and HomeController imports the package
// beside it, so the import graph is the real one only when the paths are.
//
// # The views are not here
//
// A .kyse.go opens with `//go:build kyse`, so no compiler reads one. What a
// project builds is the Go that `aru view:build` writes under
// storage/framework/views/, and running that would need the generator -- which is
// aru/internal/kyse, unimportable from another module by Go's own rule. The only
// way in is an `aru` binary on PATH, and a gate that skips whenever a binary is
// absent is what this gate was written to replace.
//
// Writing them into the module unbuilt would be worse than leaving them out, and
// that was measured rather than assumed: `go build ./...` skips a directory whose
// files are all excluded by a build constraint and exits 0, so a view naming a
// symbol that does not exist passes green. Leaving them out says what is not
// covered; writing them in would say the opposite of it.
//
// So the expressions inside the thirteen views are the hole in this gate. What
// they name is held by views_internal_test.go and by
// TestEveryScreenThisKitPublishesIsDrawnBySomething, and none of that is a type
// check.
//
// # When it skips
//
// The modules are resolved before anything is compiled, with the proxy left as
// the environment set it, so a machine with a network fills a cold cache and a
// machine without one uses what it already has. If that fails, the test skips
// saying that nothing was compiled -- a skip that reads as a pass is the defect
// this exists to prevent. The build then runs with GOPROXY=off, so a fetch
// cannot happen behind it and a fetch's error cannot be read as a compiler's.
func TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework(t *testing.T) {
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH, so nothing here was compiled and this is not a pass")
	}

	root := t.TempDir()

	// Every published file the compiler reads, enumerated from the publisher
	// rather than listed here: a tenth Go file added to the kit is covered the
	// day it is added, which is the only way this stays true.
	var published []string
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		writeInto(t, filepath.Join(root, f.Path), f.Content)
		published = append(published, path)
	}
	if len(published) == 0 {
		t.Fatal("the kit published no Go, so this gate compiled nothing")
	}

	// The one file the project owns that a published file names. HomeController
	// embeds it, and the skeleton is where it comes from -- it is an empty struct
	// there too, and nothing published calls a method on it. It is written here
	// rather than read from ../arandu on purpose: a sibling that may be absent
	// would make this gate skip, which is the thing it exists to replace.
	writeInto(t, filepath.Join(root, "app", "Http", "Controllers", "Controller.go"),
		[]byte("package controllers\n\ntype Controller struct{}\n"))

	writeInto(t, filepath.Join(root, "go.mod"), []byte(
		"module "+authSpec().ModulePath+"\n\ngo "+goDirective(t)+"\n\nrequire (\n"+
			"\tgithub.com/arandu-io/framework "+publishedFramework+"\n"+
			"\tgithub.com/arandu-io/kyse "+publishedKyse+"\n)\n"))

	download := exec.Command(tool, "mod", "download", "all")
	download.Dir = root
	download.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := download.CombinedOutput(); err != nil {
		t.Skipf("github.com/arandu-io/framework %s and github.com/arandu-io/kyse %s are not both in the "+
			"module cache and could not be fetched, so NOTHING WAS COMPILED here and this is not a pass: %v\n%s",
			publishedFramework, publishedKyse, err, out)
	}

	build := exec.Command(tool, "build", "./...")
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local")
	out, err := build.CombinedOutput()
	if err == nil {
		return
	}

	named := "none by name -- read the output below"
	if files := publishedIn(string(out), published); len(files) > 0 {
		named = strings.Join(files, "\n  ")
	}
	t.Fatalf("the Go this kit publishes does not compile against github.com/arandu-io/framework %s.\n\n"+
		"Every project that runs `go run github.com/arandu-io/ui@latest auth` receives these files, and a "+
		"project that receives them cannot build.\n\n"+
		"published file(s) the compiler named:\n  %s\n\n`go build ./...` said:\n%s\n(%v)",
		publishedFramework, named, out, err)
}

// publishedIn returns the published paths the compiler's output points at, in
// the order the kit publishes them.
//
// Matched with the colon the position carries, because the compiler writes
// path:line:column and a bare path also appears in the package header above the
// errors -- which would name every file of a package for one file's mistake.
func publishedIn(out string, published []string) []string {
	var named []string
	for _, path := range published {
		if strings.Contains(out, path+":") {
			named = append(named, path)
		}
	}
	return named
}

// goDirective is the language version this module declares.
//
// Read rather than written again, because the throwaway module above needs one
// at least as new as the framework asks for, and the day this module is bumped
// for a framework that needs it is the day that one has to move too.
func goDirective(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("this module's go.mod declares no go directive")
	return ""
}

// TestTheVersionsThisGateCompilesAgainstAreTheOnesANewProjectGets keeps the gate
// from going quietly out of date.
//
// A pin left behind still compiles, and it stops answering the question it was
// written for: what a project gets today. The skeleton's go.mod is what a new
// project starts with, so it is the answer, and this is the one check here that
// may skip -- the pins are still exercised by the gate above when the skeleton is
// not beside this module.
func TestTheVersionsThisGateCompilesAgainstAreTheOnesANewProjectGets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "arandu", "go.mod"))
	if err != nil {
		t.Skip("the skeleton is not checked out beside this module, so nothing here says whether the pinned versions are current")
	}

	for _, want := range []struct{ module, pinned string }{
		{"github.com/arandu-io/framework", publishedFramework},
		{"github.com/arandu-io/kyse", publishedKyse},
	} {
		got := requiredVersion(string(body), want.module)
		if got != want.pinned {
			t.Errorf("a new project gets %s %s and this suite compiles the published files against %s.\n"+
				"Bump the constant in publish_internal_test.go, and read what the build says about the difference.",
				want.module, got, want.pinned)
		}
	}
}

// requiredVersion reads the version a go.mod requires for one module path.
func requiredVersion(gomod, module string) string {
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1]
		}
	}
	return ""
}

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// update rewrites the golden files: go test . -update
var update = flag.Bool("update", false, "rewrite the golden files")

func authSpec() Module {
	return Module{ModulePath: "example.test/project"}
}

func Example_stampedVersion() {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.8.0"}}
	fmt.Println(resolveVersion("v9.1.0", info, true))
	// Output: v9.1.0
}

func Example_taggedModuleVersion() {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.8.0"}}
	fmt.Println(resolveVersion("dev", info, true))
	// Output: v0.8.0
}

func Example_localDevelopmentVersion() {
	fmt.Println(resolveVersion("dev", nil, false))
	fmt.Println(resolveVersion("dev", &debug.BuildInfo{}, true))
	fmt.Println(resolveVersion("dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true))
	// Output:
	// dev
	// dev
	// dev
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
	// than one that arrived: eleven plain Go files, thirteen screens, one
	// fragment, four message bodies and the one script.
	//
	// It was thirteen, and the missing nine were the ones that made the kit a
	// flow: register.kyse.go and verify.kyse.go posted to addresses nobody
	// registered, and the password reset stopped one step short of writing the
	// password. It was twenty-eight until the layout's own tag for custom.js
	// had nothing behind it in a project older than the tag.
	if len(files) != 30 {
		t.Fatalf("generated %d files, want 30", len(files))
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

// TestTheFinalSignInSeamRotatesTheSession: keeping the pre-login session id is
// session fixation. There is exactly one rotation seam, reached only after the
// password and every required factor have succeeded.
func TestTheFinalSignInSeamRotatesTheSession(t *testing.T) {
	handlers := authFile(t, "LoginController_handlers.go")

	if strings.Count(handlers, "sessions.Rotate(") != 1 ||
		!strings.Contains(bodyOf(t, handlers, "finishSignIn"), "sessions.Rotate(") {
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
	// The form, which is a file of its own: it is what comes back on a rejection,
	// so it is where the token, the swap and the absent password have to be.
	views := authFile(t, "partials/login_form.kyse.go")

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

// TestTheStarterKitDoesNotMigrate: the application's schema owns the users and
// second-factor tables. A presentation publisher emitting a second copy would
// give one table two owners and make rollout order unsafe.
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

// TestTheKitPublishesOnlyTheApplicationOwnedRouteModule pins the native
// boundary. Schema, policies and services belong to the application; this
// published module owns only HTTP routes and their presentation.
func TestTheKitPublishesOnlyTheApplicationOwnedRouteModule(t *testing.T) {
	source := authFile(t, "Auth/LoginController.go")
	for _, forbidden := range []string{
		"framework/modules/auth", "auth.Module", "auth.New(", "auth.Service",
		"auth.TenantResolver", "kernel.Migratable",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("the application-owned module publishes forbidden legacy boundary %q", forbidden)
		}
	}
	if strings.Count(source, "kernel.Module = (*Module)(nil)") != 1 {
		t.Error("the generated module must implement exactly the route-only kernel.Module contract")
	}
}

// TestTheKitBuildsItsApplicationOwnedModule pins every collaborator passed by
// the application's bootstrap.
func TestTheKitBuildsItsApplicationOwnedModule(t *testing.T) {
	source := authFile(t, "Auth/LoginController.go")
	for _, want := range []string{
		"users Users", "factors Factors", "codes onetime.CodeStore",
		"sessions *security.SessionStore", "tenant TenantResolver", "secure bool",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("New does not receive the application-owned dependency %q", want)
		}
	}
	if !strings.Contains(source, "return &Module{") {
		t.Fatal("the generated controller declares no Module constructor")
	}
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

// The thirteen screens land at the paths people look for, the fragment lands
// under partials/, and the controller lands beside them.
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
		// The one fragment, under the directory that makes it one.
		"resources/views/partials/login_form.kyse.go",
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
		if !slices.Contains(params, "authui.UserNames") {
			t.Errorf("NewHomeController does not take authui.UserNames, so the landing page cannot resolve the id in the "+
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

// constructorContract returns the constructor parameters that have to agree
// across a project and the kit that publishes into it. Concrete types keep
// their exact names. UserNames is an interface owned at the point of use, so a
// local UserNames and authui.UserNames are the same constructor seam when the
// independently compiled packages accept the same application service.
func constructorContract(t *testing.T, source, function string) []string {
	t.Helper()

	params := constructorNames(t, source, function)
	for i, param := range params {
		if param == "UserNames" || strings.HasSuffix(param, ".UserNames") {
			params[i] = "UserNames"
		}
	}
	return params
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

// TestTheProjectsInThisTreeFitTheConstructorTheKitPublishes.
//
// The drift this catches survived because nobody ran the publisher against the
// projects it publishes into. HomeController.go is replaced without a flag, so
// the moment the kit's constructor stops matching the one a project's
// bootstrap/app.go calls, publishing into that project breaks its build -- and
// nothing in either repository noticed, because each one compiles on its own.
//
// Go interfaces are structural: `UserNames` and `authui.UserNames` are not a
// mismatch merely because their use sites spell the compatible interface
// differently. The project and published-kit compile gates prove the method
// sets; this comparison keeps arity and every concrete parameter exact.
//
// The sibling skeleton is checked because every new project is a copy of it. It
// is skipped when it is not checked out beside this module, because this module
// is released on its own and its CI has only itself.
func TestTheProjectsInThisTreeFitTheConstructorTheKitPublishes(t *testing.T) {
	want := constructorContract(t, authFile(t, "HomeController.go"), "NewHomeController")

	for _, project := range []string{"arandu"} {
		t.Run(project, func(t *testing.T) {
			path := filepath.Join("..", project, "app", "Http", "Controllers", "HomeController.go")
			source, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("%s is not checked out beside this module, so nothing here proves the kit still fits it", project)
			}

			got := constructorContract(t, string(source), "NewHomeController")
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
		{"finishSignIn", `m.sessions.TakeIntended(w, r, "/")`},
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
	publishedFramework = "v0.43.0"
	publishedKyse      = "v0.16.0"
	publishedHesape    = "v0.22.0"
)

// TestEveryGoFileTheKitPublishesCompilesAgainstThePublishedFramework is the gate
// that was missing on the day render.go shipped unbuildable.
//
// SignedInName called the legacy framework service's Names method, which had
// been renamed to PublicNames and given a different signature, and every
// project that ran `go run github.com/arandu-io/ui@latest auth` received a file
// that does not build.
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
// So the expressions inside the views are the hole in this gate. What they name
// is held by views_internal_test.go and by
// TestEveryScreenThisKitPublishesIsDrawnBySomething, and none of that is a type
// check. The one structural question that CAN be answered without a compiler --
// whether a view draws a document or a piece of one -- is answered right below,
// by TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne.
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
	writeInto(t, filepath.Join(root, "app", "Models", "User.go"), []byte(`package models

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type User struct {
	ID string
	TenantID string
	Name string
	Email string
	Password string
	Roles []string
	VerifiedAt *time.Time
	CreatedAt time.Time
}

func (u User) Verified() bool { return u.VerifiedAt != nil }
func (u User) PasswordFingerprint() string {
	digest := sha256.Sum256([]byte(u.Password))
	return hex.EncodeToString(digest[:])
}
`))
	writeInto(t, filepath.Join(root, "app", "Services", "errors.go"),
		[]byte("package services\n\nimport (\"errors\"; \"strings\")\n\nvar ErrEmailTaken = errors.New(\"email taken\")\nfunc NormalizeEmail(v string) string { return strings.ToLower(strings.TrimSpace(v)) }\n"))

	writeInto(t, filepath.Join(root, "go.mod"), []byte(
		"module "+authSpec().ModulePath+"\n\ngo "+goDirective(t)+"\n\nrequire (\n"+
			"\tgithub.com/arandu-io/framework "+publishedFramework+"\n"+
			"\tgithub.com/arandu-io/hesape "+publishedHesape+"\n"+
			"\tgithub.com/arandu-io/kyse "+publishedKyse+"\n)\n"))

	download := exec.Command(tool, "mod", "download", "all")
	download.Dir = root
	download.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := download.CombinedOutput(); err != nil {
		t.Skipf("framework %s, hesape %s and kyse %s are not all in the "+
			"module cache and could not be fetched, so NOTHING WAS COMPILED here and this is not a pass: %v\n%s",
			publishedFramework, publishedHesape, publishedKyse, err, out)
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

// TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne is the gate over the
// half of the kit the compiler above cannot read.
//
// A fragment is not a fragment because a handler calls it one. The framework's
// Context.View and Context.Fragment are the same three lines around the same
// renderer -- the status differs and nothing else does -- so naming a full page
// where a fragment was wanted compiles, runs, answers the right status, and
// hands htmx a whole document for a hole the size of a form. htmx strips the
// <head> and swaps the <body>'s children in, so what lands inside the card is
// the header, the navigation and a second toaster: a page inside the page.
//
// This kit shipped exactly that. auth/login.kyse.go carried the form with
// hx-target="this" hx-swap="outerHTML", and Module.rejected answered "auth.login"
// -- the whole screen -- on every mistyped password.
//
// # What makes a view a fragment, mechanically
//
// The directory it is published in, and the source has to agree with it:
//
//	layouts/    the frame           @yield, and no layout of its own
//	partials/   a fragment          no layout and no @yield: it draws its own markup
//	mail/       a message body      no layout either -- there is no chrome in an e-mail
//	everything else, a screen       a layout, always
//
// Two other candidates were considered and are worse. "A view with no layout is
// a fragment" is what the kyse compiler already sees -- File.Extends is empty --
// but it is not enough on its own: a layout has none either, and neither does a
// mail body, so the same property covers three different things. A name suffix
// travels with the file but says nothing about where it lives, and the tree is
// what somebody opening the project reads first. The directory is the only one
// of the three that a person, this test and `aru doctor` can all see without
// opening the file.
//
// # And the rule that catches the defect above
//
// A narrowed swap belongs only in a fragment. An element carrying hx-target or
// hx-swap is asking for its own markup back rather than a document, and the view
// that draws that element is the view the server has to answer with. Written on
// a screen, it is a request no published view can satisfy -- which is exactly
// what auth/login.kyse.go was asking for.
//
// It is a sibling of the compile gate rather than lines inside it, for one
// reason: that one skips when the framework is not in the module cache and there
// is no network. This reads bytes the kit generates in-process. It must never
// have a reason to skip, and folding it in would give it one.
func TestAFragmentThisKitPublishesHasNoLayoutAndAPageHasOne(t *testing.T) {
	var checked int
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		checked++

		kind, wantsLayout, wantsYield := viewKind(path)
		body := viewBody(f.Content)

		switch hasLayout := strings.Contains(body, "@extends("); {
		case wantsLayout && !hasLayout:
			t.Errorf("%s is a %s and extends no layout: it answers a bare piece of markup where a whole "+
				"document was asked for, so a browser sent there gets a form with no header, no navigation "+
				"and no way out", path, kind)
		case !wantsLayout && hasLayout:
			t.Errorf("%s is a %s and extends a layout: what it draws is a whole document, and swapping one "+
				"into a page puts the header, the navigation and a second toaster inside it.\n"+
				"Take the @extends out, or publish it beside the screens instead", path, kind)
		}

		switch hasYield := strings.Contains(body, "@yield("); {
		case wantsYield && !hasYield:
			t.Errorf("%s is a %s and yields nothing: no view that extends it can put anything on the page", path, kind)
		case !wantsYield && hasYield:
			t.Errorf("%s is a %s and yields a section: @yield is what a layout does, and a second layout "+
				"is a second answer to what a page looks like", path, kind)
		}

		// The narrowed swap, in both spellings it has. Checked on the markup
		// with the kyse comments taken out, because the layout explains the
		// toaster by naming the attribute that fills it -- and a guard that read
		// comments would report the sentence describing the rule as a breach of
		// it.
		if kind == "fragment" {
			continue
		}
		for _, spelling := range narrowedSwap {
			if strings.Contains(body, spelling) {
				t.Errorf("%s is a %s and carries %s: that asks the server for a piece of a page, and this "+
					"file is the whole page.\n"+
					"Whatever answers it has to be a view with no layout -- publish that part under "+
					"resources/views/partials/ and draw it here with @include", path, kind, spelling)
			}
		}
	}

	if checked == 0 {
		t.Fatal("the kit published no views, so this gate read nothing")
	}
}

// narrowedSwap is every way a published view can ask for a piece of the page
// back instead of a document.
//
// Two spellings, and the second is the one this gate was blind to. The first is
// the attribute a view writes itself. The second is a prop of a kyse component:
//
//	{!! components.Button(components.ButtonProps{HxPost: ..., HxTarget: "#form"}) !!}
//
// renders hx-target= into the served document and puts neither literal string
// in this file. The gate read the source for the attribute alone, so a screen
// that narrowed its swap through a component passed -- and passed silently,
// because the attribute exists only in a rendered page and nothing here renders
// one.
//
// The props are a closed set, read off the component library rather than
// guessed: ButtonProps.HxTarget, ButtonProps.HxSwap and MenuItem.HxTarget are
// every field there that renders one of the two attributes. HxPost, HxGet and
// HxConfirm are deliberately not here -- they say where to ask and what to
// confirm first, and neither narrows what comes back.
//
// The prop names carry no colon, so no amount of space between the name and the
// value gets a screen past this. Nothing else in a published view can hold one:
// a kyse comment and an @go block are both cut before this reads anything.
var narrowedSwap = []string{"hx-target=", "hx-swap=", "HxTarget", "HxSwap"}

// viewKind says what a published view is, from where the kit publishes it, and
// what the source has to look like to be that.
//
// The path decides and the file has to agree. It is not read off the contents,
// and that is the point: a rule inferred from what a file happens to contain
// cannot be broken, because whatever the file contains becomes the rule.
func viewKind(path string) (kind string, wantsLayout, wantsYield bool) {
	switch {
	case strings.Contains(path, "/views/layouts/"):
		return "layout", false, true
	case strings.Contains(path, "/views/partials/"):
		return "fragment", false, false
	case strings.Contains(path, "/views/mail/"):
		return "message body", false, false
	default:
		return "screen", true, false
	}
}

// viewBody is the markup of a published view: everything below the package
// clause, with the @go blocks and the kyse comments taken out.
//
// All three of those hold prose, and every published file here carries prose
// about the rules it follows. A guard that read them would report the file that
// EXPLAINS a directive as a file that USES one, and the fix would be to delete
// the explanation.
func viewBody(content []byte) string {
	body := string(content)

	// The header is Go: a build tag, a package comment, the clause, an import
	// block. Cutting at the clause drops the comment with it.
	if at := strings.Index(body, "\npackage "); at >= 0 {
		if end := strings.IndexByte(body[at+1:], '\n'); end >= 0 {
			body = body[at+1+end:]
		}
	}

	body = withoutRegion(body, "@go", "@endgo")
	return withoutRegion(body, "{{--", "--}}")
}

// withoutRegion removes every open..close region, including the delimiters.
//
// Every one of them, and not the first: the layout carries several kyse
// comments, and a version that stopped after one left the rest of the file
// reading as markup.
func withoutRegion(body, opener, closer string) string {
	for {
		start := strings.Index(body, opener)
		if start < 0 {
			return body
		}
		end := strings.Index(body[start:], closer)
		if end < 0 {
			return body[:start]
		}
		body = body[:start] + body[start+end+len(closer):]
	}
}

// TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap is the state half of
// the gate above, and it covers the one boundary of the four with no compiler
// behind it.
//
// Four things in a published page can hold a value, and what tells them apart is
// when each is next drawn: a component holds nothing and is re-run wherever its
// caller is, the layout is drawn once per document and no swap redraws it, a
// screen is the whole document for one request, and a fragment is what is inside
// one swap target. Three of those seams are typed. A layout renders through
// view.Layout and a component is handed the page as components.Page, so neither
// can name a field of a screen and neither compiles if it tries. The fourth --
// the screen and the piece of it answered alone -- is one type: @include hands
// the page's own data straight through, so the fragment names the same struct
// and nothing separates page state from fragment state.
//
// What that costs is a defect with no symptom. Module.fragment answers the part
// when htmx asked for one, and on that branch the screen around it is not
// rendered at all. So a handler that fills a field only the screen draws sends
// the value inside a response whose other half the browser never had: the status
// is right, the markup in the hole is right, and the sentence is gone. Status is
// the field this would happen to first -- login.kyse.go draws it above the card,
// outside the form, which is exactly where a swap of the form does not reach.
//
// So the check is per call and not per file: for every m.fragment in the
// published Go, every field of the AuthPage literal it is given has to be one
// the named part draws. Page is skipped, because it is the chrome and the
// paragraph below is about it.
//
// A validation message is not drawn by its own name and is still drawn: the
// components ask the page through FieldError, so EmailError reaches the reader
// wherever an input is called "email". That indirection is followed here rather
// than worked around, which is what makes this stricter than a search for the
// field name and not weaker.
func TestEveryFieldAFragmentAnswerFillsIsDrawnInsideTheSwap(t *testing.T) {
	files := mustGenerateAuth(t)

	views := map[string]string{}
	for _, f := range files {
		if path := filepath.ToSlash(f.Path); strings.HasSuffix(path, ".kyse.go") {
			views[path] = viewBody(f.Content)
		}
	}
	byInput := fieldErrorNames(t, authFile(t, "page.go"))

	var checked int
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Base(path), f.Content, parser.AllErrors)
		if err != nil {
			t.Fatalf("%s does not parse: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || types.ExprString(call.Fun) != "m.fragment" || len(call.Args) != 6 {
				return true
			}

			part, ok := call.Args[4].(*ast.BasicLit)
			if !ok || part.Kind != token.STRING {
				t.Errorf("%s answers with a part whose name is built rather than written, so nothing "+
					"can follow it back to the view it draws", path)
				return true
			}
			name := strings.Trim(part.Value, `"`)
			source := "resources/views/" + strings.ReplaceAll(name, ".", "/") + ".kyse.go"
			body, published := views[source]
			if !published {
				t.Errorf("%s answers with %q and this kit publishes no %s", path, name, source)
				return true
			}
			checked++

			literal, ok := call.Args[5].(*ast.CompositeLit)
			if !ok {
				return true
			}
			drawn := fieldsRead(body)
			for _, elt := range literal.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				// Page is the chrome. It is filled on every path because the
				// other branch of this same call renders the whole screen, and
				// on this one the layout is not redrawn either -- so it is
				// outside the target by design rather than by mistake.
				if !ok || key.Name == "Page" {
					continue
				}
				if slices.Contains(drawn, key.Name) {
					continue
				}
				if input, mapped := byInput[key.Name]; mapped && strings.Contains(body, `Name: "`+input+`"`) {
					continue
				}
				t.Errorf("%s answers %s with AuthPage.%s filled in, and %s does not draw it.\n"+
					"htmx replaces the target and keeps the rest of the page, so the screen that would have "+
					"drawn it is not being rendered: the value is computed, sent and dropped, with a correct "+
					"status and correct markup in the hole.\n"+
					"Draw it in %s, or answer the whole screen.", path, name, key.Name, source, source)
			}
			return true
		})
	}

	if checked == 0 {
		t.Fatal("no published handler answers with a fragment, so this gate read nothing")
	}
}

// TestNothingTheLayoutDrawsIsRedrawnInsideASwap is the other half of the same
// contract, one level out.
//
// The layout runs once per document. A swap replaces markup inside the page and
// never re-runs it, so every value the chrome shows is the one the server gave
// when the document was fetched, for as long as the tab stays open. A value
// drawn there and again inside a swap target is therefore one value with two
// copies, and only the inner one is ever refreshed -- the page then shows both
// answers at once, and the stale one is the one in the header.
//
// hx-swap-oob is the exception and it stays one: it is the only way an answer
// reaches outside its own target, and writing it is a decision somebody takes
// rather than one they arrive at.
func TestNothingTheLayoutDrawsIsRedrawnInsideASwap(t *testing.T) {
	var layout string
	fragments := map[string]string{}
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		switch kind, _, _ := viewKind(path); kind {
		case "layout":
			layout = viewBody(f.Content)
		case "fragment":
			fragments[path] = viewBody(f.Content)
		}
	}
	if layout == "" {
		t.Fatal("the kit published no layout, so this gate read nothing")
	}
	if len(fragments) == 0 {
		t.Fatal("the kit published no fragment, so this gate read nothing")
	}

	chrome := fieldsRead(layout)
	for path, body := range fragments {
		if strings.Contains(body, "hx-swap-oob") {
			continue
		}
		for _, name := range fieldsRead(body) {
			if !slices.Contains(chrome, name) {
				continue
			}
			t.Errorf("%s draws .%s and so does the layout.\n"+
				"The layout is rendered once, with the document, and no swap re-runs it -- so the copy in the "+
				"chrome keeps whatever it said then while this one is replaced on every answer, and the page "+
				"shows two values for one fact.\n"+
				"Draw it in one of the two, or reach the other with hx-swap-oob.", path, name)
		}
	}
}

// TestNoPublishedViewKeepsStateInTheBrowser holds the last corner of the
// contract: the client owns nothing a framework would hold for it.
//
// State on this stack is the server's. A handler reads the form, decides, and
// answers markup that is already correct, so there is no second copy in the
// browser to keep in step and nothing to reconcile when the two disagree. An
// hx- attribute is not a counter-example and cannot become one: hx-post,
// hx-target and hx-swap say where to ask and what to replace, and nothing reads
// a value back out of one.
//
// What the browser does own is what dies with the tab and the server never
// needs to hear about -- a menu that is open, a row that is selected -- and it
// has a home already: ui.js, which the layout loads. It binds on document and
// dispatches on data- attributes, keeps open and selected in the ARIA the
// markup already carries, and evaluates nothing, so the DOM is the only copy
// and swapped-in markup is live where it lands.
//
// The attributes below are what somebody reaches for instead, and in a view
// this kit publishes they do nothing at all: none of the scripts the layout
// loads reads one, and a framework that compiles a directive out of a string
// could not run beside them, because the policy is script-src 'self' with no
// unsafe-eval. A rule of business written in one is a fragment nobody asked the
// server for.
func TestNoPublishedViewKeepsStateInTheBrowser(t *testing.T) {
	held := []string{
		"x-data", "x-init", "x-effect", "x-model", "x-show", "x-bind",
		"x-text", "x-html", "x-ref", "x-for", "x-if", "x-on:", "$store",
	}

	var checked int
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		checked++

		body := viewBody(f.Content)
		for _, attribute := range held {
			if attributeIn(body, attribute) {
				t.Errorf("%s carries %s, which holds a value in the browser.\n"+
					"What a screen shows came from the server in the answer it is part of, so a copy here is "+
					"one nothing reconciles. It is also inert: none of the scripts the layout loads reads that "+
					"attribute, and a framework that compiles a directive out of a string could not run beside "+
					"them -- the policy is script-src 'self' with no unsafe-eval.\n"+
					"Put the decision in a handler and swap the markup. What dies with the tab is ui.js's, on "+
					"a data- attribute, or <details> and :focus-within", path, attribute)
			}
		}
	}

	if checked == 0 {
		t.Fatal("the kit published no views, so this gate read nothing")
	}
}

// fieldsRead is every name a view draws off the struct it renders from.
//
// A dot opens one only when what precedes it is not part of an identifier:
// `{{ .PageTitle() }}` and `@if(!.SignedIn())` are the page, and view.URL,
// components.FieldProps and "htmx.min.js" are not. That is why this is one scan
// rather than a search per name -- searching for Email finds it inside
// EmailError, and a field then reads as drawn because a different one is.
func fieldsRead(body string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(body); i++ {
		if body[i] != '.' || (i > 0 && isNamePart(body[i-1])) {
			continue
		}
		end := i + 1
		for end < len(body) && isNamePart(body[end]) {
			end++
		}
		if end == i+1 {
			continue
		}
		if name := body[i+1 : end]; !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = end - 1
	}
	slices.Sort(out)
	return out
}

// attributeIn reports whether the markup carries this attribute, rather than a
// longer one ending in its name.
//
// The character before it is the whole check: without it hx-data would report
// x-data, and the difference between an attribute that routes a swap and one
// that holds a value is the subject of the test that calls this.
func attributeIn(body, name string) bool {
	for at := 0; at < len(body); {
		i := strings.Index(body[at:], name)
		if i < 0 {
			return false
		}
		i += at
		if i == 0 || (!isNamePart(body[i-1]) && body[i-1] != '-') {
			return true
		}
		at = i + len(name)
	}
	return false
}

// isNamePart reports whether c can appear inside an identifier.
func isNamePart(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// TestTheKitsLayoutKeepsWhatTheSkeletonsLayoutCarries.
//
// The layout is in `replaced`: publishing overwrites the skeleton's with no flag
// at all. So an element the skeleton's head had and this one does not is one
// every project silently loses the first time it runs this command, and losing
// it is invisible -- the page still renders, and what went is a script nobody
// looks for in a document that never had it.
//
// This test was named in the doc comment of the layout template for a long while
// and did not exist, and the head had drifted by three elements by the time it
// was written:
//
//   - ui.js, which is where every client behaviour on this stack lives. Without
//     it a menu does not open and a copy button does not copy, on every screen
//     of every project that published the kit.
//   - includeIndicatorStyles, without which htmx injects a <style> element the
//     policy refuses -- style-src 'self', no unsafe-inline -- once per page.
//   - the second icon, which is the hazard the layout's own doc comment names.
//
// Elements and not bytes: the two files are different designs on purpose, so the
// navigation, the widths and the wording are theirs to differ on. What may not
// differ is what the head loads and declares, which is read here as one key per
// element -- an asset by the name view.URL is given, an icon by its address, a
// meta by the name or property that identifies it plus its content when that is
// a constant.
//
// What it does not reach: an attribute beside the identifying one. The two icons
// are compared by address, so a sizes= or a type= dropped from one of them is a
// difference this passes over. Naming the element is what the hazard is about --
// a tag that went entirely is the one nobody notices.
//
// It skips where the skeleton is not beside this module, like the other checks
// that read a sibling: this module is released alone and its CI has only itself.
func TestTheKitsLayoutKeepsWhatTheSkeletonsLayoutCarries(t *testing.T) {
	skeleton, err := os.ReadFile(filepath.Join("..", "arandu", "resources", "views", "layouts", "app.kyse.go"))
	if err != nil {
		t.Skip("the skeleton is not checked out beside this module, so nothing here says whether the head still carries what its own does")
	}

	want := headElements(string(skeleton))
	if len(want) == 0 {
		t.Fatal("the skeleton's layout declares no head elements; either it changed shape or this test is looking in the wrong place")
	}
	got := headElements(authView(t, "layouts/app.kyse.go"))

	for _, element := range want {
		if slices.Contains(got, element) {
			continue
		}
		t.Errorf("the skeleton's layout carries %s in its head and this kit's does not.\n"+
			"Publishing replaces that file with no flag, so every project loses it the first time this "+
			"command is run -- and loses it silently, because a document that never had an element does "+
			"not look wrong.\n"+
			"Add it here, or take it out of the skeleton: those are the two ways the two agree.", element)
	}
}

// headElements is one key per element the <head> of a layout declares.
//
// A key and not the line, because the two layouts indent and order differently
// and neither of those is what this is about. What identifies an element is what
// it brings: the asset name inside view.URL for a script or a stylesheet, the
// address for an icon, and the name or property for a meta.
//
// Anything inside a kyse comment is skipped, because both files explain in prose
// the very tags they carry -- and one of them explains a tag it deliberately
// does not.
func headElements(layout string) []string {
	head := layout
	if at := strings.Index(head, "<head>"); at >= 0 {
		head = head[at:]
	}
	if end := strings.Index(head, "</head>"); end >= 0 {
		head = head[:end]
	}
	head = withoutRegion(head, "{{--", "--}}")
	head = withoutRegion(head, "<!--", "-->")

	var out []string
	seen := map[string]bool{}
	add := func(key string) {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for _, element := range strings.Split(head, "<")[1:] {
		element = strings.SplitN(element, ">", 2)[0]
		switch {
		case strings.HasPrefix(element, "script"), strings.HasPrefix(element, "link"):
			if name, ok := assetName(element); ok {
				add(strings.Fields(element)[0] + " " + name)
				continue
			}
			if href, ok := attributeValue(element, "href"); ok {
				add(strings.Fields(element)[0] + " " + href)
			}
		case strings.HasPrefix(element, "meta"):
			for _, identifier := range []string{"name", "property"} {
				value, ok := attributeValue(element, identifier)
				if !ok {
					continue
				}
				// The content joins the key when it is a constant, which
				// catches a setting that changed as well as a tag that went.
				// htmx-config is the one that matters -- it decides what a 422
				// means and whether htmx injects a stylesheet the policy
				// refuses -- and it is a setting, not a design choice, so the
				// two files have no business differing on it. A content the
				// page fills in is left out: og:title is the title, and the
				// two layouts are not obliged to word it alike.
				content, filled := attributeValue(element, "content")
				if filled && !strings.Contains(content, "{{") {
					value += " " + content
				}
				add("meta " + value)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

// assetName is the name a tag asks view.URL for, and false for a tag that names
// an address of its own instead.
func assetName(element string) (string, bool) {
	const call = `view.URL("`
	at := strings.Index(element, call)
	if at < 0 {
		return "", false
	}
	rest := element[at+len(call):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// TestEveryAssetAPublishedViewAsksForIsOneSomethingRegisters is the gate over
// the tags a published layout writes.
//
// view.URL panics on a name nothing registered, and that is its design rather
// than an oversight: a plausible URL for a file the binary does not carry is a
// 404 on every page that nobody reads, where a refusal names the missing asset
// once. So every view.URL in a published view is a requirement on the project
// the view lands in -- and resources/views/layouts/app.kyse.go is in
// `replaced`, which means it overwrites a project's own with no flag at all.
//
// A name nothing answers is therefore not a broken image. It is every request
// of every project that ran this command, answered with a panic, chosen by
// nobody: the person did not opt in to the layout, and the failure arrives at
// the first page load rather than at the build.
//
// This shipped. The layout gained a tag for custom.js while the only package
// registering that name was one the skeleton had started carrying the same day,
// so publishing into any project generated before that wrote a layout which
// could not render.
//
// Two sources answer for a name, and the difference between them is what makes
// this gate work:
//
//   - What the runtime embeds. That set is copied here, and it is the only
//     copied thing in this test: this module declares no dependency, so it has
//     nothing to ask. It is named in the failure so that a reader can compare
//     it against the release rather than trust it.
//   - What this kit itself publishes, read out of the RegisterAsset calls in
//     the Go it writes. Derived and not listed, which is the half that matters:
//     dropping the file that registers custom.js turns this test red instead of
//     leaving it green against a name typed into a slice here.
func TestEveryAssetAPublishedViewAsksForIsOneSomethingRegisters(t *testing.T) {
	// Registered by hesape/view's own init, from files embedded at build time.
	// Nothing an application does removes one.
	//
	// The dev server registers one more, arandu-reload.js, and it is left out
	// deliberately: it exists only while `aru dev` is running, so a published
	// view that referenced it would be a tag that panics in production. Seeing
	// it listed in a runtime refusal is not a reason to add it here.
	embedded := map[string]bool{
		"app.css":            true,
		"basecoat.bundle.js": true,
		"htmx.min.js":        true,
		"theme.js":           true,
		"ui.js":              true,
	}

	files := mustGenerateAuth(t)

	delivered := map[string]bool{}
	for _, f := range files {
		for _, name := range callNames(string(f.Content), "view.RegisterAsset") {
			delivered[name] = true
		}
	}

	var asked int
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		for _, name := range callNames(string(f.Content), "view.URL") {
			asked++
			if embedded[name] || delivered[name] {
				continue
			}
			t.Errorf("%s asks view.URL for %q, and nothing registers that name.\n"+
				"The runtime embeds: %s.\nThis kit publishes a registration for: %s.\n"+
				"view.URL refuses a name nothing registered, so this is not a missing file -- it is every "+
				"request of every project that receives this view, answered with a panic, and the layout is "+
				"replaced with no flag so nobody chose it.\n"+
				"Publish the package that registers it, or drop the tag.",
				path, name, sortedNames(embedded), sortedNames(delivered))
		}
	}
	if asked == 0 {
		t.Fatal("no published view asks view.URL for anything, so this gate read nothing: either the layout " +
			"stopped loading its assets, or the scan is looking in the wrong place")
	}
}

// TestEveryFileTheKitEmbedsIsOnePublishedBesideItAndNotEmpty.
//
// The two ways a published //go:embed hurts somebody else and nobody here. A
// directive naming a file the kit does not write is a project that does not
// build, and one naming a file the kit writes empty is a project that builds
// and panics at start-up, because RegisterAsset refuses a zero-byte body -- a
// script tag that loads and runs nothing reads exactly like a behaviour
// somebody forgot to register.
//
// Neither is visible from this repository without reading for it: nothing here
// compiles what it publishes, and the golden files compare bytes against bytes.
func TestEveryFileTheKitEmbedsIsOnePublishedBesideItAndNotEmpty(t *testing.T) {
	files := mustGenerateAuth(t)

	byPath := map[string][]byte{}
	for _, f := range files {
		byPath[filepath.ToSlash(f.Path)] = f.Content
	}

	var checked int
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		for _, line := range strings.Split(string(f.Content), "\n") {
			name, ok := strings.CutPrefix(strings.TrimSpace(line), "//go:embed ")
			if !ok {
				continue
			}
			checked++
			// The pattern is relative to the file carrying it and cannot name a
			// parent directory, which is the whole reason the registration
			// package sits under resources/ beside the script rather than with
			// the controllers.
			embedded := filepath.ToSlash(filepath.Join(filepath.Dir(path), strings.TrimSpace(name)))
			body, published := byPath[embedded]
			switch {
			case !published:
				t.Errorf("%s embeds %s and this kit does not publish it: the project receives Go that does "+
					"not build, naming a file it was never given", path, embedded)
			case len(body) == 0:
				t.Errorf("%s embeds %s and this kit publishes it empty: RegisterAsset refuses a zero-byte "+
					"body, so the application panics when it starts", path, embedded)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no published Go embeds anything, so this gate read nothing: the script the layout asks for " +
			"reaches the binary through an embed, and a kit that publishes none is not delivering it")
	}
}

// callNames is every name passed as the first argument of a call, wherever that
// argument is written as a string literal.
//
// A scan and not a parser, because most of what it reads is kyse: below the
// package clause a .kyse.go is markup, and no Go parser accepts one. The shape
// is safe to read this way -- view.URL's own documentation requires a constant
// argument, and a registration names the file it embeds.
func callNames(source, call string) []string {
	var out []string
	prefix := call + `("`
	for {
		at := strings.Index(source, prefix)
		if at < 0 {
			return out
		}
		source = source[at+len(prefix):]
		end := strings.IndexByte(source, '"')
		if end < 0 {
			return out
		}
		out = append(out, source[:end])
		source = source[end:]
	}
}

// sortedNames is a set as one readable line, for a failure message.
func sortedNames(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	if len(out) == 0 {
		return "nothing"
	}
	return strings.Join(out, ", ")
}

// attributeValue is the value of one attribute of a tag, in either quote.
func attributeValue(element, name string) (string, bool) {
	for _, quote := range []string{`="`, `='`} {
		at := strings.Index(element, name+quote)
		if at < 0 {
			continue
		}
		rest := element[at+len(name)+len(quote):]
		end := strings.IndexByte(rest, quote[1])
		if end < 0 {
			continue
		}
		return rest[:end], true
	}
	return "", false
}

// TestTheStateScannersFindTheShapesTheyGuardAgainst.
//
// Both scanners above answer a question about text, and a scanner that answers
// "nothing here" for every input passes on a tree that is full of what it is
// looking for -- reading as a green check either way. So each is given the shape
// it exists to find and the shapes it must not mistake for it.
//
// The pairs that matter are the last two of each: .Email must not be found
// inside .EmailError, because a field would then read as drawn when a different
// one is; and x-data must not be found inside hx-data, because the whole subject
// here is the difference between an attribute that routes a swap and one that
// holds a value.
func TestTheStateScannersFindTheShapesTheyGuardAgainst(t *testing.T) {
	t.Run("fieldsRead", func(t *testing.T) {
		for _, c := range []struct {
			name, markup string
			want         []string
		}{
			{"an interpolated method is the page", "<title>{{ .PageTitle() }}</title>", []string{"PageTitle"}},
			{"so is a field in a directive", "@if(!.SignedIn())", []string{"SignedIn"}},
			{"a selector on a package is not", `<link href="{{ view.URL("htmx.min.js") }}">`, nil},
			{"and neither is the page handed on as a whole", "{!! components.Field(components.FieldProps{Page: .}) !!}", nil},
			{"a longer name does not hide inside a shorter one", "{{ .EmailError }}", []string{"EmailError"}},
			{"nor a shorter one inside a longer", "{{ .Email }}{{ .EmailError }}", []string{"Email", "EmailError"}},
		} {
			t.Run(c.name, func(t *testing.T) {
				got := fieldsRead(c.markup)
				if !slices.Equal(got, c.want) {
					t.Errorf("read %v, want %v", got, c.want)
				}
			})
		}
	})

	t.Run("attributeIn", func(t *testing.T) {
		for _, c := range []struct {
			name, markup, attribute string
			want                    bool
		}{
			{"the attribute itself", `<div x-data="{ open: false }">`, "x-data", true},
			{"written on a line of its own", "<div\n\tx-show=\"open\">", "x-show", true},
			{"an htmx attribute is not it", `<form hx-swap="outerHTML" hx-data="">`, "x-data", false},
			{"and neither is a longer name ending in it", `<div data-x-data="">`, "x-data", false},
		} {
			t.Run(c.name, func(t *testing.T) {
				if got := attributeIn(c.markup, c.attribute); got != c.want {
					t.Errorf("found %v, want %v", got, c.want)
				}
			})
		}
	})
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
// not beside this module. CI resolves the latest published skeleton instead of
// skipping, because the isolated runner is exactly where this drift used to
// pass unnoticed.
func TestTheVersionsThisGateCompilesAgainstAreTheOnesANewProjectGets(t *testing.T) {
	body, err := currentSkeletonGoMod()
	if err != nil {
		message := fmt.Sprintf("the current skeleton go.mod is unavailable, so nothing here says whether the pinned versions are current: %v", err)
		if os.Getenv("CI") != "" {
			t.Fatal(message)
		}
		t.Skip(message)
	}

	for _, want := range []struct{ module, pinned string }{
		{"github.com/arandu-io/framework", publishedFramework},
		{"github.com/arandu-io/hesape", publishedHesape},
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

func currentSkeletonGoMod() ([]byte, error) {
	path := filepath.Join("..", "arandu", "go.mod")
	body, err := os.ReadFile(path)
	if err == nil {
		return body, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read sibling skeleton: %w", err)
	}

	tool, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("find go: %w", err)
	}

	download := exec.Command(tool, "mod", "download", "-json", "github.com/arandu-io/arandu@latest")
	download.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := download.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("download current skeleton: %w: %s", err, out)
	}

	var module struct {
		GoMod string
		Error string
	}
	if err := json.Unmarshal(out, &module); err != nil {
		return nil, fmt.Errorf("decode current skeleton metadata: %w", err)
	}
	if module.Error != "" {
		return nil, fmt.Errorf("download current skeleton: %s", module.Error)
	}
	if module.GoMod == "" {
		return nil, fmt.Errorf("download current skeleton: go.mod path is empty")
	}

	body, err = os.ReadFile(module.GoMod)
	if err != nil {
		return nil, fmt.Errorf("read downloaded skeleton go.mod: %w", err)
	}
	return body, nil
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

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The password flow, held to the properties it is published for.
//
// Every check here is a defect that shipped. The reset kept its tokens in a
// package-level map, mailed whatever address was typed without looking it up,
// consumed the token before validating the password, carried no tenant, and left
// every other session of the account signed in. The confirmation screen was
// published with no route, no handler and a form that posted to itself.

// bodyOf returns the source of one top-level function of a generated file, so a
// test about the ORDER of two calls reads the function and not the file.
func bodyOf(t *testing.T, source, function string) string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, function+".go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated file does not parse: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		return source[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset]
	}
	t.Fatalf("the generated file declares no %s", function)
	return ""
}

// TestTheResetLinkIsSignedAndHeldNowhere.
//
// The store was a package-level map guarded by a mutex, and it cost four things
// at once: a restart threw away every link in flight, a second replica refused
// the link the first one issued, every address anybody typed left an entry that
// only a click could remove, and asking twice left two live links. What answers
// all four is here -- a token signed with the application key, carrying what it
// needs, stored nowhere.
func TestTheResetLinkIsSignedAndHeldNowhere(t *testing.T) {
	source := authFile(t, "PasswordController.go")

	for _, gone := range []string{"sync.Mutex", "map[string]", "byHash", "crypto/rand"} {
		if strings.Contains(source, gone) {
			t.Errorf("the reset still keeps %s: a token this process holds is a token the other replica has never "+
				"heard of, and one a restart throws away", gone)
		}
	}
	if !strings.Contains(source, "m.signer.Sign(resetPurpose, auth.ResetPayload(u), resetTTL)") {
		t.Error("the link is not signed with the reset purpose and the payload: the purpose is what keeps a " +
			"verification link from working as a password reset, since the same key signs both")
	}
}

// TestNothingIsMailedToAnAddressNobodyLookedUp.
//
// The guard was `if email != ""` and the send was unconditional, which made this
// endpoint a way to send mail from the application's own domain to any address
// on request. The account is looked up first, and nothing goes out when there
// is none.
func TestNothingIsMailedToAnAddressNobodyLookedUp(t *testing.T) {
	source := authFile(t, "PasswordController.go")

	send := bodyOf(t, source, "sendPasswordLink")
	if !strings.Contains(send, "m.auth.FindForReset(") {
		t.Fatal("the handler mails without looking the account up first")
	}
	if strings.Contains(send, ".Send(") {
		t.Error("the handler sends inline: the send belongs behind the lookup, in the branch that found a user")
	}
	if !strings.Contains(bodyOf(t, source, "sendPasswordReset"), ".Send(") {
		t.Error("nothing sends the message at all")
	}
}

// TestTheResetIsThrottledByTheCounterSigningInAlreadyUses.
//
// Reusing security.SignInThrottle through the service, rather than a counter of
// this screen's own: the screen is the project's file the moment it is
// published, and a control that a redesign of the form can delete is not a
// control.
func TestTheResetIsThrottledByTheCounterSigningInAlreadyUses(t *testing.T) {
	source := authFile(t, "PasswordController.go")

	if !strings.Contains(source, "middleware.KeyByIP(r)") {
		t.Error("nothing keys the reset by where the request came from, so it is not throttled at all")
	}
	if !strings.Contains(source, "auth.TooManyAttemptsError") {
		t.Error("the screen never answers a lockout, so a throttled request would look like a link that was sent")
	}
	for _, second := range []string{"time.Ticker", "attempts[", "map[string]int"} {
		if strings.Contains(source, second) {
			t.Errorf("the screen keeps %s: a second counter beside the framework's is a second answer to "+
				"\"how often may this be asked\"", second)
		}
	}
}

// TestNothingIsConsumedUntilThePasswordIsAcceptable.
//
// The token used to be consumed first, so a password two characters short burned
// the link -- and the form that came back carried a token that no longer
// existed, which reads as the application losing the reset rather than as a rule
// about length.
func TestNothingIsConsumedUntilThePasswordIsAcceptable(t *testing.T) {
	body := bodyOf(t, authFile(t, "PasswordController.go"), "updatePassword")

	length := strings.Index(body, "security.MinPasswordLen")
	match := strings.Index(body, "password != confirmation")
	verify := strings.Index(body, "m.signer.Verify(")
	write := strings.Index(body, "m.auth.ResetPassword(")

	switch {
	case length < 0 || match < 0 || verify < 0 || write < 0:
		t.Fatalf("updatePassword no longer does all four of match, length, verify and write:\n%s", body)
	case match > verify || length > verify:
		t.Error("the link is verified before the password is checked, so a rejected password spends the link")
	case verify > write:
		t.Error("the password is written before the signature is checked")
	}
}

// TestTheResetSaysTheSameThingWhetherTheAddressIsRegisteredOrNot.
//
// The anti-enumeration property is that the two answers are the same bytes. A
// sentence naming the account is one request per address and no guessing left in
// it -- which is a better oracle than the sign-in form, and the sign-in form has
// a whole comment about not being one.
//
// Registration is the deliberate exception and is not read here: somebody who
// cannot continue has to be told why, and the answer is the same one they would
// get by trying to sign in.
func TestTheResetSaysTheSameThingWhetherTheAddressIsRegisteredOrNot(t *testing.T) {
	enumerating := []string{
		"no such account", "that address is not registered", "we have no account",
		"unknown address", "no account with that address",
	}

	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.Contains(path, "PasswordController.go") && !strings.Contains(path, "views/auth/passwords/") {
			continue
		}
		body := strings.ToLower(string(f.Content))
		for _, leak := range enumerating {
			if strings.Contains(body, leak) {
				t.Errorf("%s says %q, which confirms which addresses exist one request at a time", f.Path, leak)
			}
		}
	}

	// And the one sentence it does answer with is written once, so the two
	// branches cannot drift apart the first time somebody rewords one of them.
	source := authFile(t, "PasswordController.go")
	if strings.Count(source, `"If that address is registered, a link is on its way."`) != 1 {
		t.Error("the answer is spelled more than once: two literals a hundred lines apart are two answers")
	}
}

// TestAResetEndsTheAccountsOtherSessions.
//
// The person asking for a reset is often asking because somebody else is signed
// in as them. Leaving the other sessions open leaves that person exactly where
// they were, holding a cookie that still works -- which the handler's own
// comment used to say, as a limitation.
func TestAResetEndsTheAccountsOtherSessions(t *testing.T) {
	body := bodyOf(t, authFile(t, "PasswordController.go"), "updatePassword")

	if !strings.Contains(body, "m.sessions.DestroyOthers(") {
		t.Fatal("a completed reset leaves every session of the account signed in")
	}
	if !strings.Contains(body, `DestroyOthers(r.Context(), auth.SubjectOf(u), "")`) {
		t.Error("the reset keeps a session: there is none on this request worth keeping, and the one that must " +
			"stop working belongs to whoever forced the reset")
	}
}

// TestTheResetFormCarriesTheAddressItWasSentTo.
//
// The screen asks for an address and marks it Required. It was neither filled in
// nor read, so the one field somebody had to type on a form reached from their
// own inbox changed nothing at all.
func TestTheResetFormCarriesTheAddressItWasSentTo(t *testing.T) {
	source := authFile(t, "PasswordController.go")

	if !strings.Contains(bodyOf(t, source, "showPasswordReset"), "auth.ResetAddress(payload)") {
		t.Error("the reset form is drawn without the address the link was minted for")
	}
	if !strings.Contains(bodyOf(t, source, "updatePassword"), `r.PostFormValue("email")`) {
		t.Error("the address the form asks for is discarded, which makes a Required input decoration")
	}
}

// TestTheConfirmationScreenHasARouteAHandlerAndAnAddressToPostTo.
//
// It was published, compiled and registered, and unreachable: PasswordConfirmURL
// was declared, read by the template and assigned nowhere, so the form rendered
// action="" and posted to itself. There was no route and no handler either --
// under a command that prints "every screen has a route and every route has a
// handler".
func TestTheConfirmationScreenHasARouteAHandlerAndAnAddressToPostTo(t *testing.T) {
	routes := bodyOf(t, authFile(t, "Auth/LoginController.go"), "Routes")
	for _, want := range []string{
		`g.Get("/password/confirm", m.showPasswordConfirm`,
		`g.Post("/password/confirm", m.confirmPassword`,
	} {
		if !strings.Contains(routes, want) {
			t.Errorf("no route for %s", want)
		}
	}
	if !strings.Contains(routes, "middleware.RequireAuth(m.sessions)") {
		t.Error("the confirmation screen is not behind the session guard: there is nothing to confirm without one")
	}

	source := authFile(t, "PasswordController.go")
	for _, want := range []string{"func (m *Module) showPasswordConfirm(", "func (m *Module) confirmPassword("} {
		if !strings.Contains(source, want) {
			t.Errorf("the handler %s does not exist", want)
		}
	}
	// Through the service, not through Authenticate: that one is the sign-in
	// path and takes an address rather than a subject.
	if !strings.Contains(source, "m.auth.ConfirmPassword(") {
		t.Error("the confirmation does not go through the service, so it is not throttled -- an unlimited " +
			"\"is this the password?\" behind a session is a password oracle for a stolen cookie")
	}
	if !strings.Contains(source, "m.sessions.Confirm(") {
		t.Error("a correct password does not stamp the session, so the guard in front of it would ask again forever")
	}
}

// TestTheRememberBoxIsReadAndSurvivesARejection.
//
// The checkbox has been drawn since the first version of this kit and nothing
// read it: AuthPage.Remember was never assigned, so the box was always drawn
// unticked and did not even come back after a wrong password.
func TestTheRememberBoxIsReadAndSurvivesARejection(t *testing.T) {
	handlers := authFile(t, "LoginController_handlers.go")

	if !strings.Contains(handlers, `r.PostFormValue("remember")`) {
		t.Fatal("nothing reads the remember-me box, so ticking it does nothing at all")
	}
	if !strings.Contains(handlers, "security.Remember(remember)") {
		t.Error("the answer is read and not passed to the session, so the session still lives for the plain ttl")
	}
	if !strings.Contains(bodyOf(t, handlers, "rejected"), "Remember:") {
		t.Error("a rejected sign-in loses the box, and nothing on screen says it was unticked")
	}
}

// TestEveryAddressAScreenReadsIsFilledInSomewhere is the general form of the two
// bugs above.
//
// A URL field declared on AuthPage, read by a template and assigned by nobody
// renders as action="" or href="" -- a control that lies, and one that no
// compiler complains about because the zero value of a string is a valid string.
// This is the check that would have caught PasswordConfirmURL and DashboardURL
// on the commit that introduced them.
func TestEveryAddressAScreenReadsIsFilledInSomewhere(t *testing.T) {
	files := mustGenerateAuth(t)

	// The fields AuthPage adds. view.Page's own are the framework's and are
	// filled in by m.page.
	fields := urlFields(t, authFile(t, "page.go"))
	if len(fields) == 0 {
		t.Fatal("AuthPage declares no URL field; either it changed shape or this test is looking in the wrong place")
	}

	var goSource, markup strings.Builder
	for _, f := range files {
		if strings.HasSuffix(filepath.ToSlash(f.Path), ".kyse.go") {
			markup.Write(f.Content)
			continue
		}
		goSource.Write(f.Content)
	}

	for _, name := range fields {
		read := strings.Contains(markup.String(), "."+name)
		filled := strings.Contains(goSource.String(), "data."+name+" = ") ||
			strings.Contains(goSource.String(), name+": ")

		switch {
		case read && !filled:
			t.Errorf("AuthPage.%s is read by a screen and assigned nowhere: that screen draws an empty address, "+
				"and a form with action=\"\" posts to itself", name)
		case filled && !read:
			t.Errorf("AuthPage.%s is filled in and no screen reads it: delete it, or the next person will assume "+
				"a page uses it", name)
		}
	}
}

// urlFields returns the names of the AuthPage fields that hold an address.
func urlFields(t *testing.T, page string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "page.go", page, parser.AllErrors)
	if err != nil {
		t.Fatalf("page.go does not parse: %v", err)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "AuthPage" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, f := range structType.Fields.List {
			for _, name := range f.Names {
				if strings.HasSuffix(name.Name, "URL") {
					out = append(out, name.Name)
				}
			}
		}
		return false
	})
	return out
}

// TestNothingTheKitPublishesIsBrandedWithItsOwnName.
//
// The verification mail carried the literal word "Arandu" in its header, so
// every project that ran this command sent its first message to its own users
// signed with the name of the framework. The brand is a field, filled from the
// configuration the module was built with, and the mail views name that field --
// a word typed into published markup is the kit's word going out on somebody
// else's mail.
func TestNothingTheKitPublishesIsBrandedWithItsOwnName(t *testing.T) {
	for _, f := range mustGenerateAuth(t) {
		if strings.Contains(string(f.Content), "Arandu") {
			t.Errorf("%s ships the word Arandu: the name in a published file is the framework's name, "+
				"printed to the users of an application that is not it", f.Path)
		}
	}

	for _, view := range []string{"verify-email.kyse.go", "password-reset.kyse.go"} {
		if !strings.Contains(authFile(t, view), "Brand:     .BrandName") {
			t.Errorf("%s does not draw the brand from the message it renders, so whatever name it shows "+
				"is one this file decided for every project", view)
		}
	}
	for _, mailable := range []string{"VerifyEmail.go", "PasswordReset.go"} {
		if !strings.Contains(authFile(t, mailable), "BrandName string") {
			t.Errorf("%s carries no BrandName, so the view has nothing to draw but a literal", mailable)
		}
	}
	if body := bodyOf(t, authFile(t, "RegisterController.go"), "sendVerification"); !strings.Contains(body, "BrandName: m.appName") {
		t.Error("the verification mail goes out without the application's name: the field exists and nobody fills it")
	}
	if body := bodyOf(t, authFile(t, "PasswordController.go"), "sendPasswordReset"); !strings.Contains(body, "BrandName: m.appName") {
		t.Error("the reset mail goes out without the application's name: the field exists and nobody fills it")
	}
}

// TestBothMessagesAreBuiltTheSameWay.
//
// One of them was mailui and the other was 40 lines of hand-written <table>,
// with its own greys, its own radius and its own footer -- two designs from one
// application, in the same inbox, a week apart. What a message is made of is
// decided in the component library and nowhere else.
func TestBothMessagesAreBuiltTheSameWay(t *testing.T) {
	for _, view := range []string{"verify-email.kyse.go", "password-reset.kyse.go"} {
		source := authFile(t, view)
		if !strings.Contains(source, "mailui.Layout(mailui.LayoutProps{") {
			t.Errorf("%s does not go through mailui, so this message is a second design of the same thing", view)
		}
		if strings.Contains(source, "<table") {
			t.Errorf("%s builds its own table: the box model of an inbox is mailui's problem, and solving it "+
				"twice is how two messages stop looking alike", view)
		}
		// The wording is the project's, and a republish must not take it back.
		if !strings.Contains(source, "{{-- arandu:begin custom --}}") {
			t.Errorf("%s has no custom block, so republishing the kit overwrites whatever this application "+
				"decided to say to its own users", view)
		}
	}
}

// TestARejectedFormIsNeverAnswered200.
//
// screenStatus exists because HTMX swaps the fragment of a 422 and of a 200
// alike: answering 200 to a forged link leaves the browser, the log and every
// dashboard agreeing that it worked, and nothing looks wrong until somebody asks
// why the verification rate is 100%. m.screen is 200 by definition, so a screen
// carrying a validation message must not be drawn through it.
func TestARejectedFormIsNeverAnswered200(t *testing.T) {
	for _, name := range []string{"PasswordController.go", "RegisterController.go", "LoginController_handlers.go"} {
		source := authFile(t, name)
		file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.AllErrors)
		if err != nil {
			t.Fatalf("%s does not parse: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "screen" {
				return true
			}
			lit, ok := call.Args[len(call.Args)-1].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || !strings.HasSuffix(key.Name, "Error") {
					continue
				}
				t.Errorf("%s draws a screen carrying %s through m.screen, which is 200: a refusal answered 200 is "+
					"a refusal nothing downstream can count", name, key.Name)
			}
			return true
		})
	}
}

// TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn is the general form of
// TestEveryAddressAScreenReadsIsFilledInSomewhere, for the sentences rather than
// the addresses.
//
// A handler computed "Your password has been changed. Sign in with it.", passed
// it to a view with no block to draw it, and the string was thrown away. Nothing
// failed: the screen rendered, the password really had changed, and the person
// was shown an ordinary sign-in form with no reason to believe any of it had
// worked. The same hole put somebody who clicked a dead verification link on the
// cheerful "check your inbox" page.
func TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn(t *testing.T) {
	files := mustGenerateAuth(t)

	fields := messageFields(t, authFile(t, "page.go"))
	if len(fields) == 0 {
		t.Fatal("AuthPage declares no message field; either it changed shape or this test is looking in the wrong place")
	}

	var goSource, markup strings.Builder
	for _, f := range files {
		if strings.HasSuffix(filepath.ToSlash(f.Path), ".kyse.go") {
			markup.Write(f.Content)
			continue
		}
		goSource.Write(f.Content)
	}

	// A field reaches the screen one of two ways. Status is read straight from
	// the markup, as `.Status`. A validation message is not: the components ask
	// the page through FieldError, so the field is drawn when FieldError maps a
	// form field name onto it AND some screen has an input by that name.
	//
	// That indirection is why this check is stronger than the one it replaces
	// rather than weaker. Before, `.NameError` appearing anywhere in the markup
	// counted as drawn. Now the name has to line up end to end: handler fills
	// the field, FieldError names it, a screen has an input called that.
	names := fieldErrorNames(t, authFile(t, "page.go"))

	for _, name := range fields {
		filled := strings.Contains(goSource.String(), name+":")

		read := strings.Contains(markup.String(), "."+name)
		if !read {
			if formField, mapped := names[name]; mapped {
				read = strings.Contains(markup.String(), `Name: "`+formField+`"`) ||
					strings.Contains(markup.String(), `Name:  "`+formField+`"`)
			}
		}

		switch {
		case filled && !read:
			t.Errorf("a handler fills AuthPage.%s and no screen draws it: the sentence is computed, passed and "+
				"thrown away, and the person is told nothing", name)
		case read && !filled:
			t.Errorf("a screen draws AuthPage.%s and no handler fills it: delete it, or the next person will "+
				"assume something writes it", name)
		}
	}
}

// fieldErrorNames reads AuthPage.FieldError and returns, for each message field
// it answers with, the form field name that reaches it.
//
// It parses rather than greps because the mapping is the load-bearing half of
// the indirection: a case that returns the wrong field is a message shown under
// the wrong input, and nothing else would catch it.
func fieldErrorNames(t *testing.T, page string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "page.go", page, parser.AllErrors)
	if err != nil {
		t.Fatalf("page.go does not parse: %v", err)
	}

	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		decl, ok := n.(*ast.FuncDecl)
		if !ok || decl.Name.Name != "FieldError" || decl.Recv == nil {
			return true
		}
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok || len(clause.List) != 1 || len(clause.Body) != 1 {
				return true
			}
			lit, ok := clause.List[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			ret, ok := clause.Body[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			sel, ok := ret.Results[0].(*ast.SelectorExpr)
			if !ok {
				return true
			}
			formField, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			out[sel.Sel.Name] = formField
			return true
		})
		return false
	})

	if len(out) == 0 {
		t.Fatal("AuthPage.FieldError maps no field: either it changed shape or this test is looking in the wrong place")
	}
	return out
}

// messageFields returns the names of the AuthPage fields that hold a sentence
// for the reader.
func messageFields(t *testing.T, page string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "page.go", page, parser.AllErrors)
	if err != nil {
		t.Fatalf("page.go does not parse: %v", err)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "AuthPage" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, f := range structType.Fields.List {
			for _, name := range f.Names {
				if name.Name == "Status" || strings.HasSuffix(name.Name, "Error") {
					out = append(out, name.Name)
				}
			}
		}
		return false
	})
	return out
}

// TestSigningInLandsOnThePageTheGuardTurnedAwayFrom.
//
// middleware.RequireAuth writes the address it refused into a signed cookie, and
// the sign-in handler is the only thing that spends it. The kit's ended with
// redirect(w, r, "/"), while the framework's own sign-in handler -- the one this
// kit REPLACES -- already ended with TakeIntended. So publishing the starter kit
// removed the behaviour from the project it was published into, the same shape
// the missing guest guard had: every guard went on remembering, nothing ever
// read it, and somebody who followed a link to one invoice signed in and landed
// on the front page to go and find it again.
//
// The fallback has to stay a fallback. TakeIntended proves the address is local
// before it hands it back, so the destination is never a URL somebody else chose.
func TestSigningInLandsOnThePageTheGuardTurnedAwayFrom(t *testing.T) {
	body := bodyOf(t, authFile(t, "LoginController_handlers.go"), "doLogin")

	if !strings.Contains(body, `m.sessions.TakeIntended(w, r, "/")`) {
		t.Error("doLogin does not spend the address the guard remembered.\n" +
			"Every RequireAuth refusal writes one, and this is the only handler that reads it: " +
			"without this, following a link while signed out and then signing in ends on the front page, " +
			"and the person has to find the page again.")
	}
	if strings.Contains(body, "redirect(w, r, \"/\")") {
		t.Error("doLogin still redirects to a fixed address, so the remembered one is dropped")
	}
}

// TestEveryScreenOfTheKitDrawsTheHalfOfTheHeaderThatMatchesTheSession.
//
// The layout draws Login and Register, or the name and a sign-out form, from
// view.Page.Authenticated -- and Module.page left it at false for every one of
// the nine screens. Two of them are only ever reached WITH a session:
// /auth/password/confirm sits behind middleware.RequireAuth, and the verify
// notice is where an unverified account is sent. The screen whose entire job is
// to ask somebody to prove they are still there offered them a Login button.
//
// It is checked on Module.page and not on each handler on purpose: page is the
// one place the chrome is built, and a handler that filled the two fields itself
// would be the second place the header is decided.
func TestEveryScreenOfTheKitDrawsTheHalfOfTheHeaderThatMatchesTheSession(t *testing.T) {
	source := authFile(t, "render.go")

	file, err := parser.ParseFile(token.NewFileSet(), "render.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated chrome does not parse: %v", err)
	}

	fields := compositeKeys(t, file, "page", "ChromeProps")
	for _, want := range []string{"Authenticated", "UserName"} {
		if fields[want] == "" {
			t.Errorf("Module.page does not fill %s, so every screen of this kit renders the guest header.\n"+
				"It fills %v. A signed-in person on /auth/password/confirm is shown a Login button and no way to sign out.",
				want, keysOf(fields))
		}
	}

	// Read from the session and from nothing else: who is asking is not
	// something a request may state.
	if body := bodyOf(t, source, "page"); !strings.Contains(body, "m.sessions.Load(r.Context(), r)") {
		t.Error("Module.page decides who is signed in without loading the session")
	}
}

// TestEveryScreenThisKitPublishesIsDrawnBySomething is the general form of the
// URL test, for views.
//
// The command prints "Every screen has a route and every route has a handler".
// welcome.kyse.go was published, compiled into its own package, blank-imported
// by the instructions and rendered by nothing at all -- the landing page with
// the Login and Register buttons on it, unreachable, while home.kyse.go
// ("Dashboard. You are logged in.") was drawn for signed-in people and guests
// alike. It is the same defect the confirmation screen had, and it
// survived the pass that fixed that one because nothing asked the question about
// every screen at once.
//
// So it is asked about every screen at once: a view this kit writes has to be
// named by a Go file this kit writes.
func TestEveryScreenThisKitPublishesIsDrawnBySomething(t *testing.T) {
	files := mustGenerateAuth(t)

	named := map[string]bool{}
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Base(path), f.Content, parser.AllErrors)
		if err != nil {
			t.Fatalf("%s does not parse: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, name := range renderedNames(fn) {
				named[name] = true
			}
		}
	}

	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		name, ok := screenName(path)
		if !ok {
			continue
		}
		if !named[name] {
			t.Errorf("%s is published and no handler names %q.\n"+
				"It compiles, it is blank-imported by the wiring this command prints, and nobody can reach it -- "+
				"under a command whose own instructions say every screen has a route.", path, name)
		}
	}
}

// renderedNames is every view one function actually draws.
//
// Only the name argument of a render call counts, and not every string literal
// in the file: a screen mentioned in a comment, in a route or in a variable that
// nothing reads is exactly the shape welcome.kyse.go had, and a test that
// accepted a mention would have passed over it.
//
// A name held in a variable is followed one step, because HomeController picks
// between two screens with one -- and one step is where it stops: a name this
// test cannot see is a name the next reader cannot see either.
func renderedNames(fn *ast.FuncDecl) []string {
	// Where a view's name sits in each of the calls that draw one. m.fragment
	// carries two, and both count: it is given the screen and the one part of it
	// that is answered alone, and it draws whichever the request asked for. A
	// table with only the first would report the fragment as published and drawn
	// by nothing, which is the defect this whole test is about.
	position := map[string][]int{
		"ctx.View":       {0},
		"m.screen":       {2},
		"m.screenStatus": {3},
		"m.fragment":     {3, 4},
	}

	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		at, ok := position[types.ExprString(call.Fun)]
		if !ok {
			return true
		}
		for _, i := range at {
			if len(call.Args) <= i {
				continue
			}
			switch arg := call.Args[i].(type) {
			case *ast.BasicLit:
				out = append(out, strings.Trim(arg.Value, `"`))
			case *ast.Ident:
				out = append(out, literalsAssignedTo(fn, arg.Name)...)
			}
		}
		return true
	})
	return out
}

// literalsAssignedTo is every string this function puts in one variable.
func literalsAssignedTo(fn *ast.FuncDecl, name string) []string {
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || ident.Name != name || i >= len(assign.Rhs) {
				continue
			}
			if lit, ok := assign.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				out = append(out, strings.Trim(lit.Value, `"`))
			}
		}
		return true
	})
	return out
}

// screenName is the name a handler renders a published view by, and reports
// false for the files that are not screens.
//
// The layout is drawn by @extends and never by name, and a message body is named
// by its mailable's Content(). Neither is reachable by a route, so neither is
// what the test above is about.
func screenName(path string) (string, bool) {
	const dir = "resources/views/"
	if !strings.HasPrefix(path, dir) || !strings.HasSuffix(path, ".kyse.go") {
		return "", false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(path, dir), ".kyse.go")
	if strings.HasPrefix(rest, "layouts/") || strings.HasPrefix(rest, "mail/") {
		return "", false
	}
	return strings.ReplaceAll(rest, "/", "."), true
}

// TestNeitherTokenSurvivesBeingSerialized.
//
// Two of the types this kit publishes hold a secret. AuthPage carries the
// one-time token of a password reset link, which is the whole credential --
// whoever reads it sets that account's password -- and it carries the session's
// CSRF token through the view.Page it embeds. ChromeProps carries that same CSRF
// token. Both are ordinary structs a handler holds for the length of a request,
// and the debug page prints a recorded value with json.MarshalIndent, so one
// observability.Dump of either one published a token somebody can spend.
//
// It is proved by running the published bytes and not by reading them: the
// question is what json.Marshal and slog actually produce, and a method that
// names the right fields and copies the wrong ones reads correct. The kit is
// compiled in a module of its own against the checkouts beside this one, and
// skipped where they are absent -- this module is released alone, and its CI has
// only itself.
func TestNeitherTokenSurvivesBeingSerialized(t *testing.T) {
	const (
		reset = "reset-token-a-stranger-can-spend"
		csrf  = "csrf-token-read-off-a-screenshot"
	)

	out := runInPublishedKit(t, []string{"encoding/json", "log/slog", "os"}, `
	page := authui.AuthPage{ResetToken: `+strconv.Quote(reset)+`}
	page.Page.Token = `+strconv.Quote(csrf)+`
	chrome := authui.ChromeProps{Token: `+strconv.Quote(csrf)+`}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(page); err != nil {
		panic(err)
	}
	if err := enc.Encode(chrome); err != nil {
		panic(err)
	}

	// The other half. A log line handed the whole value goes through
	// slog.LogValuer, and a type that does not implement it is printed with %+v
	// -- every field of it, tokens included.
	slog.New(slog.NewTextHandler(os.Stdout, nil)).Info("dump", "page", page, "chrome", chrome)
`)

	for _, secret := range []string{reset, csrf} {
		if strings.Contains(out, secret) {
			t.Errorf("a token reaches the output of the published kit:\n%s", out)
		}
	}

	// The marker, so that a program which printed nothing at all cannot pass:
	// both types name their token and give it the marker rather than dropping
	// it, because a field that vanished reads exactly like one nobody filled in.
	if want := `"ResetToken":"[redacted]"`; !strings.Contains(out, want) {
		t.Errorf("the reset token is not reported as %s; the page serialized to:\n%s", want, out)
	}
	if want := `"Token":"[redacted]"`; !strings.Contains(out, want) {
		t.Errorf("the CSRF token is not reported as %s; the page serialized to:\n%s", want, out)
	}
}

// registration is one route the published Routes method mounts: how it is
// reached, and the name a URL can be built from. An empty name is a screen no
// template can link to without writing the address out.
type registration struct{ method, path, name string }

// publishedRoutes reads the registrations out of the Routes method the kit
// publishes, in the order they are written.
//
// It reads the statements of that method and not the whole file, because the
// question is what a project mounts: a registration written anywhere else is one
// nothing ever calls.
func publishedRoutes(t *testing.T) []registration {
	t.Helper()

	source := authFile(t, "Auth/LoginController.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "LoginController.go", source, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated file does not parse: %v", err)
	}

	var routes *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Routes" {
			routes = fn
		}
	}
	if routes == nil {
		t.Fatal("the generated file declares no Routes")
	}

	verbs := map[string]bool{"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true}

	var out []registration
	for _, stmt := range routes.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}

		// Name(...) wraps the registration, so the registration is what it is
		// called on. Unwrapped, the route carries no name at all.
		name := ""
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Name" {
			name = stringLiteral(t, call.Args[0])
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			call = inner
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !verbs[sel.Sel.Name] {
			continue
		}
		out = append(out, registration{
			method: sel.Sel.Name,
			path:   stringLiteral(t, call.Args[0]),
			name:   name,
		})
	}
	return out
}

// stringLiteral is the value of a literal argument, and fails the test on
// anything else: a path or a name assembled at runtime is one no reader and no
// tool can follow back to the screen it belongs to.
func stringLiteral(t *testing.T, expr ast.Expr) string {
	t.Helper()

	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("expected a string literal and found %T", expr)
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy.
//
// The kit takes Routes over from the framework's auth module, and for a long
// while it named nothing. So "auth.login" resolved in a project that had not run
// this command and stopped resolving in one that had -- the address unchanged,
// the name gone, and a template that built a URL from it broken by an install
// whose promise is that the screens answer where the minimal ones did.
//
// The two the framework also mounts carry the framework's names, and that is the
// whole criterion: a substitution that renames what it replaces changes the
// contract without saying so. The rest are this kit's own and read the same way.
//
// A POST that shares its address with the GET beside it is left unnamed, which
// is why three rows here have none. The path built from the GET's name is
// already where the form posts, and a second name for one address is a choice
// nobody can make correctly.
//
// The table is exact, order included, so that a route added to the kit is a row
// somebody wrote here rather than a screen that quietly arrives unnamed.
func TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy(t *testing.T) {
	want := []registration{
		{"Get", "/login", "auth.login"},
		{"Post", "/login", ""},
		{"Post", "/logout", "auth.logout"},

		{"Get", "/password", "auth.password.request"},
		{"Post", "/password/email", "auth.password.email"},
		{"Get", "/password/reset", "auth.password.reset"},
		{"Post", "/password/update", "auth.password.update"},
		{"Get", "/password/confirm", "auth.password.confirm"},
		{"Post", "/password/confirm", ""},

		{"Get", "/register", "auth.register"},
		{"Post", "/register", ""},
		{"Get", "/verify", "auth.verify.notice"},
		{"Get", "/verify/confirm", "auth.verify.confirm"},
		{"Post", "/verify/resend", "auth.verify.resend"},
	}

	got := publishedRoutes(t)
	if len(got) != len(want) {
		t.Fatalf("the kit mounts %d routes and this test knows %d: a screen was added or removed, and the "+
			"decision about its name belongs in the table above\n%+v", len(got), len(want), got)
	}
	for i, route := range got {
		if route == want[i] {
			continue
		}
		if route.method == want[i].method && route.path == want[i].path {
			t.Errorf("%s %s is named %q and must be named %q", route.method, route.path, route.name, want[i].name)
			continue
		}
		t.Errorf("route %d is %s %s and this test expects %s %s", i, route.method, route.path,
			want[i].method, want[i].path)
	}
}

// TestTheNamesSurviveTheSubstitution.
//
// The half of the question no reading of the source answers: after the kit's
// module has replaced the framework's, does URL("auth.login") still resolve, and
// to the same address.
//
// The framework's names are read off its own router rather than restated here,
// so this test keeps agreeing with the framework when the framework changes. The
// two constants are checked for the same reason: they are what the guards
// redirect to, and a screen the guards cannot reach is a redirect to a 404.
//
// It is skipped where the sibling checkouts are absent, like the other compiled
// test -- this module is released alone, and its CI has only itself.
func TestTheNamesSurviveTheSubstitution(t *testing.T) {
	out := runInPublishedKit(t, []string{
		"fmt",
		"github.com/arandu-io/framework/http",
		"github.com/arandu-io/framework/http/middleware",
		"github.com/arandu-io/framework/modules/auth",
	}, `
	published := http.NewRouter()

	// Nothing here serves a request, so the zero value is the module: Routes
	// reads one field of it, and only to hand the session store to two guards.
	(&authui.Module{}).Routes(published)

	// The framework's module, on a router of its own, so that the names it gives
	// are read and not restated. A service over a nil handle is enough to
	// register routes, and registering is all this does.
	bare := http.NewRouter()
	auth.New(auth.NewService(auth.NewUserRepo(nil), nil, nil), auth.FixedTenant("")).Routes(bare)

	for _, route := range bare.Routes() {
		name := route.RouteName()
		if name == "" {
			continue
		}
		was, err := bare.Table().URL(name)
		if err != nil {
			panic(err)
		}
		now, err := published.Table().URL(name)
		if err != nil {
			panic(fmt.Sprintf("the kit answers where %s did and dropped the name: %v", name, err))
		}
		if now != was {
			panic(fmt.Sprintf("%s was %s and the kit moved it to %s", name, was, now))
		}
		fmt.Printf("kept %s=%s\n", name, now)
	}

	for _, name := range []string{
		"auth.password.request", "auth.password.email", "auth.password.reset",
		"auth.password.update", "auth.password.confirm",
		"auth.register", "auth.verify.notice", "auth.verify.confirm", "auth.verify.resend",
	} {
		path, err := published.Table().URL(name)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s=%s\n", name, path)
	}

	if got := published.Table().Must("auth.login"); got != middleware.SignInPath {
		panic(fmt.Sprintf("auth.login is %s and every guard sends people to %s", got, middleware.SignInPath))
	}
	if got := published.Table().Must("auth.password.confirm"); got != middleware.PasswordConfirmPath {
		panic(fmt.Sprintf("auth.password.confirm is %s and RequireConfirmedPassword sends people to %s",
			got, middleware.PasswordConfirmPath))
	}
`)

	for _, want := range []string{
		"kept auth.login=/auth/login",
		"kept auth.logout=/auth/logout",
		"auth.password.request=/auth/password",
		"auth.password.email=/auth/password/email",
		"auth.password.reset=/auth/password/reset",
		"auth.password.update=/auth/password/update",
		"auth.password.confirm=/auth/password/confirm",
		"auth.register=/auth/register",
		"auth.verify.notice=/auth/verify",
		"auth.verify.confirm=/auth/verify/confirm",
		"auth.verify.resend=/auth/verify/resend",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is not among what the published kit resolved:\n%s", want, out)
		}
	}
}

// runInPublishedKit compiles the Go this kit publishes into a module of its own
// and runs one statement block against it, returning everything the program
// wrote.
//
// A module of its own, rather than a package in this one: this module declares
// no dependency on the framework, and that is deliberate -- `go run
// github.com/arandu-io/ui@latest auth` adds nothing to anybody's go.mod, and a
// dependency taken here for a test would be one taken there for real.
//
// imports are the paths the block needs, without an alias -- the published
// package is always imported, as authui.
func runInPublishedKit(t *testing.T, imports []string, body string) string {
	t.Helper()

	replaces := map[string]string{}
	for _, name := range []string{"framework", "kyse", "hesape"} {
		path, err := filepath.Abs(filepath.Join("..", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Skipf("%s is not checked out beside this module, so nothing here can compile what the kit publishes", name)
		}
		replaces[name] = path
	}

	root := t.TempDir()
	for _, f := range mustGenerateAuth(t) {
		// The Go under app/, which is everything the kit declares a type in. A
		// view is markup behind a build tag, and HomeController extends a base
		// class that belongs to the skeleton rather than to the kit.
		path := filepath.ToSlash(f.Path)
		if !strings.HasPrefix(path, "app/") || strings.HasSuffix(path, ".kyse.go") ||
			strings.HasSuffix(path, "HomeController.go") {
			continue
		}
		writeInto(t, filepath.Join(root, f.Path), f.Content)
	}

	gomod := "module " + authSpec().ModulePath + "\n\ngo 1.26\n\nrequire (\n" +
		"\tgithub.com/arandu-io/framework v0.0.0\n\tgithub.com/arandu-io/kyse v0.0.0\n)\n\n"
	for _, name := range []string{"framework", "kyse", "hesape"} {
		gomod += "replace github.com/arandu-io/" + name + " => " + replaces[name] + "\n"
	}
	writeInto(t, filepath.Join(root, "go.mod"), []byte(gomod))

	block := ""
	for _, path := range imports {
		block += "\t" + strconv.Quote(path) + "\n"
	}
	writeInto(t, filepath.Join(root, "cmd", "probe", "main.go"), []byte(
		"package main\n\nimport (\n"+block+"\n\tauthui \""+
			authSpec().ModulePath+"/app/Http/Controllers/Auth\"\n)\n\nfunc main() {\n"+body+"}\n"))

	cmd := exec.Command("go", "run", "./cmd/probe")
	cmd.Dir = root
	// No workspace: the tree this runs in has one, and it does not list a
	// directory that did not exist a moment ago. The replaces above are what
	// points at the siblings instead.
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the published kit: %v\n%s", err, out)
	}
	return string(out)
}

func writeInto(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

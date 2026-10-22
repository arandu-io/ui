package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// TestTheRegistrationInputLogsOnlyPresence keeps both password fields behind a
// structured-log boundary. The input itself may be attached to a log record,
// but neither credentials nor account PII may cross that boundary.
func TestTheRegistrationInputLogsOnlyPresence(t *testing.T) {
	source := authFile(t, "RegisterController.go")
	logValue := bodyOf(t, source, "LogValue")

	for _, want := range []string{
		`slog.Bool("name_supplied", in.Name != "")`,
		`slog.Bool("email_supplied", in.Email != "")`,
		`slog.Bool("password_supplied", in.Password != "")`,
		`slog.Bool("password_confirmation_supplied", in.PasswordConfirmation != "")`,
	} {
		if !strings.Contains(logValue, want) {
			t.Errorf("registrationInput.LogValue does not publish the safe signal %q", want)
		}
	}
	for _, secret := range []string{
		`slog.String("name"`, `slog.String("email"`, `slog.String("password"`,
		`slog.String("password_confirmation"`,
	} {
		if strings.Contains(logValue, secret) {
			t.Errorf("registrationInput.LogValue exposes a submitted value through %q", secret)
		}
	}
}

// TestTheSignUpFormAsksForAPasswordTwiceUnlessTheProjectSaysOtherwise.
//
// The setting this reads is an addition, and an addition must not move what
// somebody already has. A project that publishes this kit and changes nothing
// gets the form it got before: a name, an address, a password, a confirmation,
// all four required, and a handler that rejects a pair that differs.
//
// Read off the published bytes rather than off the setting's name alone,
// because the bytes are what a project receives. The middle check is the one
// that survives a rename: PasswordTwice is the ZERO value of the type, so a
// field of it that nobody set is the option that asks for the most rather than
// the one that asks for nothing.
func TestTheSignUpFormAsksForAPasswordTwiceUnlessTheProjectSaysOtherwise(t *testing.T) {
	controller := authFile(t, "RegisterController.go")

	if !strings.Contains(controller, "const registrationAsks = PasswordTwice") {
		t.Error("the published registration handler does not default to asking for a password twice: this " +
			"setting is an addition, and a project that changes nothing has to receive what it had")
	}
	if !strings.Contains(controller, "PasswordTwice RegistrationCredential = iota") {
		t.Error("PasswordTwice is not the zero value of RegistrationCredential: the value nobody set has to " +
			"be the one that asks for the most, never the one that asks for nothing")
	}

	register := authView(t, "auth/register.kyse.go")
	for _, input := range []string{"name", "email", "password", "password_confirmation"} {
		if !strings.Contains(register, `Name: "`+input+`", Label:`) {
			t.Errorf("the sign-up screen no longer draws %q", input)
		}
	}
	if required := strings.Count(register, "Required: true"); required != 4 {
		t.Errorf("the sign-up screen marks %d inputs required, want 4: an option that changed how the "+
			"default form behaves is not an option", required)
	}

	doRegister := bodyOf(t, controller, "doRegister")
	for _, rule := range []string{
		`checkNewPassword(errs, "password", in.Password)`,
		"in.Password != in.PasswordConfirmation",
	} {
		if !strings.Contains(doRegister, rule) {
			t.Errorf("the registration handler no longer applies %q", rule)
		}
	}
}

// TestTheSignUpFormAndItsHandlerAskForTheSameThing.
//
// Drawing a box the handler ignores is a field that does nothing. Requiring one
// the form does not draw is worse: every submission rejected, with the message
// hung on an input that is not on the page, and a green build either way.
// Switching the password off in one of the two and not the other is the defect
// this kit already had, inverted.
//
// So each password input the screen draws sits inside an @if on a page field,
// and each password rule the handler applies sits behind the predicate that
// fills that same field. One value decides both, and this reads the published
// bytes to hold the two ends of it together.
func TestTheSignUpFormAndItsHandlerAskForTheSameThing(t *testing.T) {
	register := authView(t, "auth/register.kyse.go")
	doRegister := bodyOf(t, authFile(t, "RegisterController.go"), "doRegister")

	for _, c := range []struct {
		input, drawnWhen, validatedWhen, rule string
	}{
		{
			"password", "@if(.AsksForPassword())", "registrationAsks.asksForPassword()",
			`checkNewPassword(errs, "password", in.Password)`,
		},
		{
			"password_confirmation", "@if(.AsksForPasswordConfirmation())",
			"registrationAsks.asksForConfirmation()", "in.Password != in.PasswordConfirmation",
		},
	} {
		at := strings.Index(register, c.drawnWhen)
		if at < 0 {
			t.Errorf("the sign-up screen draws %q unconditionally: an application that asks for no password "+
				"still shows the box, and the form and the handler disagree", c.input)
			continue
		}
		// The guarded region is what the @if opens and the first @endif closes.
		// An input drawn after that endif is one the screen shows whatever the
		// handler decided, which is the half of the disagreement that looks
		// right in review.
		guarded := register[at:]
		if end := strings.Index(guarded, "@endif"); end >= 0 {
			guarded = guarded[:end]
		}
		if !strings.Contains(guarded, `Name: "`+c.input+`"`) {
			t.Errorf("the %q input is not inside %s: the guard has to be what decides whether the box is "+
				"drawn, not a block beside it", c.input, c.drawnWhen)
		}

		if !guardedBy(doRegister, c.validatedWhen, c.rule) {
			t.Errorf("the registration handler applies %q without asking %s first: a rule on an input the "+
				"form does not draw rejects every submission, pointing at a field nobody can see",
				c.rule, c.validatedWhen)
		}
	}
}

// TestASignUpScreenNobodyFilledInStillAsksForBothBoxes.
//
// The two files do not travel together. page.go is in `replaced`, so publishing
// overwrites it with no flag; RegisterController.go is not, so publishing keeps
// it. `auth --views --force` therefore writes a new sign-up screen beside a
// handler that predates the setting and fills neither field -- and --views is
// the flag whose whole purpose is to be the safe one.
//
// What that project has to get is the form it had. It does, because the fields
// are stored as negatives and read through methods: false is "draw the box",
// and false is what a handler that never heard of them leaves behind. Spelled
// the positive way, the same republish would have quietly stopped asking for a
// password on a screen that still posts to a handler demanding one.
func TestASignUpScreenNobodyFilledInStillAsksForBothBoxes(t *testing.T) {
	page := authFile(t, "Auth/page.go")

	for _, want := range []string{
		"func (p AuthPage) AsksForPassword() bool { return !p.WithoutPasswordBox }",
		"func (p AuthPage) AsksForPasswordConfirmation() bool { return !p.WithoutConfirmationBox }",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("AuthPage does not answer the sign-up form through the negated field:\n  want %s\n"+
				"The zero value of this struct has to be the form that asks, because publishing replaces "+
				"page.go and keeps the handler that fills it", want)
		}
	}

	// And the screen has to ask through those methods rather than read the
	// fields, or the negation above is a comment rather than a mechanism.
	register := authView(t, "auth/register.kyse.go")
	for _, field := range []string{".WithoutPasswordBox", ".WithoutConfirmationBox"} {
		if strings.Contains(register, field) {
			t.Errorf("the sign-up screen reads %s directly: it has to ask through the method, which is where "+
				"the negation lives", field)
		}
	}
}

// TestNoPublishedHandlerPutsAnEmptyPasswordIntoAComparison.
//
// The hazard that arrives with an optional password, and it is worse than the
// problem it solves: an account created without one, whose password column ends
// up holding the hash of the empty string, is an account anybody signs in to by
// submitting nothing.
//
// Two guards stand between this kit and that, and only one of them is here. The
// far side is the native provider, which refuses an account whose password
// column is empty whatever is offered against it -- so a credential that is
// ABSENT is safe by construction, which is why the setting asks an application
// for absence rather than for the hash of nothing.
//
// The near side is these three handlers, and it is what this reads. Each
// refuses an empty password BEFORE the call that compares it, so the offered
// half is never empty either. Removing one of those guards is what would turn a
// stored hash of the empty string from unreachable into a sign-in.
//
// The fourth check is the registration handler itself: with the box switched
// off it clears what the body carried before calling Register, so a forged
// submission cannot set a credential through a field the application does not
// show.
func TestNoPublishedHandlerPutsAnEmptyPasswordIntoAComparison(t *testing.T) {
	for _, c := range []struct {
		file, handler, guard, compares string
	}{
		{"LoginController_handlers.go", "doLogin", `in.Password == ""`, "m.users.VerifyCredentials("},
		{"LoginController_handlers.go", "doLogin", `trimmed != ""`, "m.verifyTrimmedPassword("},
		{"PasswordController.go", "confirmPassword", `in.Password == ""`, "m.users.ConfirmPassword("},
		{
			"PasswordController.go", "updatePassword",
			`checkNewPassword(errs, "password", in.Password)`, "m.users.ResetPassword(",
		},
	} {
		body := bodyOf(t, authFile(t, c.file), c.handler)
		guard, compares := strings.Index(body, c.guard), strings.Index(body, c.compares)
		switch {
		case compares < 0:
			t.Errorf("%s no longer calls %s, so this gate is reading a handler that stopped being the one "+
				"that compares a password", c.handler, c.compares)
		case guard < 0:
			t.Errorf("%s hands a password to %s with no %s in front of it: an account whose stored hash is "+
				"the hash of the empty string is then one anybody signs in to by submitting nothing",
				c.handler, c.compares, c.guard)
		case guard > compares:
			t.Errorf("%s checks %s only after calling %s, and by then the comparison has happened",
				c.handler, c.guard, c.compares)
		}
	}

	doRegister := bodyOf(t, authFile(t, "RegisterController.go"), "doRegister")
	cleared := strings.Index(doRegister, `in.Password, in.PasswordConfirmation = "", ""`)
	registered := strings.Index(doRegister, "m.users.Register(")
	switch {
	case registered < 0:
		t.Error("the registration handler no longer calls Users.Register, so this gate is reading the wrong " +
			"handler")
	case cleared < 0:
		t.Error("the registration handler passes on a password the form did not draw: with the box switched " +
			"off, a forged body would still set the credential on the new account")
	case cleared > registered:
		t.Error("the registration handler drops the undrawn password only after registering with it")
	}

	// The reset's guard is checkNewPassword, and the policy inside it is not a
	// guard against the empty password: its length rule, like every rule but
	// required, passes a value that is absent. So the refusal of "" has to come
	// before the policy is asked.
	check := bodyOf(t, authFile(t, "RegisterController.go"), "checkNewPassword")
	empty, policy := strings.Index(check, `if password == ""`), strings.Index(check, ".Passes(")
	switch {
	case policy < 0:
		t.Error("checkNewPassword no longer asks passwordPolicy, so the checklist the screens draw and the " +
			"rule the handlers apply are two declarations again")
	case empty < 0 || empty > policy:
		t.Error("checkNewPassword leaves the empty password to the policy, which passes it: an empty box " +
			"would reach the user service, and the reset would spend its code on a password nobody can store")
	}
}

// guardedBy reports whether rule, in a handler's body, runs only when cond
// holds: written as `if cond && rule` or as the first statement of `if cond {`.
func guardedBy(body, cond, rule string) bool {
	if strings.Contains(body, cond+" && "+rule) {
		return true
	}
	at := strings.Index(body, "if "+cond+" {")
	if at < 0 {
		return false
	}
	rest := strings.TrimLeft(body[at+len("if "+cond+" {"):], " \t\n")
	return strings.HasPrefix(rest, rule)
}

// TestThePendingSignInRedactsLogsWithoutChangingItsSignedProtocol separates
// observability from serialization: logs expose only presence, while the JSON
// signed into the short-lived cookie must retain the real password fingerprint.
func TestThePendingSignInRedactsLogsWithoutChangingItsSignedProtocol(t *testing.T) {
	source := authFile(t, "TwoFactorController.go")
	logValue := bodyOf(t, source, "LogValue")

	if !strings.Contains(logValue, `slog.Bool("password_fingerprint_present", p.PasswordFingerprint != "")`) {
		t.Error("pendingSignIn.LogValue does not replace the password fingerprint with a presence signal")
	}
	if strings.Contains(logValue, `slog.String("password_fingerprint"`) ||
		strings.Contains(logValue, `slog.Any("password_fingerprint"`) {
		t.Error("pendingSignIn.LogValue exposes the password fingerprint")
	}

	for _, protocol := range []string{
		`PasswordFingerprint string ` + "`json:\"password_fingerprint\"`",
		`PasswordFingerprint: account.Fingerprint(u.PasswordHash())`,
		`json.Marshal(pendingSignIn{`,
	} {
		if !strings.Contains(source, protocol) {
			t.Errorf("the signed pending-sign-in protocol lost %q", protocol)
		}
	}
	if strings.Contains(source, "func (p pendingSignIn) MarshalJSON") {
		t.Error("pendingSignIn redacts its signed JSON instead of redacting only its log representation")
	}
}

// TestTheResetUsesOnlyPurposeBoundNativeCodes rejects every remnant of the
// former signed-link protocol and pins the native single-use CodeStore seam.
func TestTheResetUsesOnlyPurposeBoundNativeCodes(t *testing.T) {
	source := authFile(t, "PasswordController.go")

	for _, gone := range []string{"m.signer.Sign", "ResetPayload", "ResetToken", `name="token"`, `?token=`} {
		if strings.Contains(source, gone) {
			t.Errorf("the reset still publishes the legacy signed-link boundary %q", gone)
		}
	}
	if !strings.Contains(source, `m.codes.Issue(ctx, resetPurpose, resetCodeSubject(u))`) ||
		!strings.Contains(source, `resetPurpose = "reset-password"`) {
		t.Error("password reset is not issued through the purpose-bound native CodeStore")
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

	send := bodyOf(t, source, "sendPasswordCode")
	if !strings.Contains(send, "m.users.Lookup(") {
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

	if !strings.Contains(source, "m.codes.Issue(") || !strings.Contains(source, "m.codes.Consume(") {
		t.Error("the reset bypasses the native store that owns cooldown, attempts, expiry and atomic consumption")
	}
	for _, second := range []string{"time.Ticker", "attempts[", "map[string]int", "sync.Mutex"} {
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

	length := strings.Index(body, `checkNewPassword(errs, "password", in.Password)`)
	match := strings.Index(body, "in.Password != in.PasswordConfirmation")
	consume := strings.Index(body, "m.codes.Consume(")
	write := strings.Index(body, "m.users.ResetPassword(")

	switch {
	case length < 0 || match < 0 || consume < 0 || write < 0:
		t.Fatalf("updatePassword no longer does all four of match, length, consume and write:\n%s", body)
	case match > consume || length > consume:
		t.Error("the code is consumed before the password is acceptable")
	case consume > write:
		t.Error("the password is written before the one-time code is consumed")
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
	if strings.Count(source, `"If that address is registered, a code is on its way."`) != 1 {
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
	if !strings.Contains(body, `DestroyOthers(r.Context(), subject, "")`) {
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

	if !strings.Contains(bodyOf(t, source, "showPasswordReset"), `r.URL.Query().Get("email")`) {
		t.Error("the reset form does not accept the address as non-secret convenience data")
	}
	if !strings.Contains(source, `form:"email"`) ||
		!strings.Contains(bodyOf(t, source, "updatePassword"), "m.users.Lookup(r.Context(), m.tenant(r), in.Email)") {
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
		`g.Action(stdhttp.MethodPost, "/password/confirm", m.confirmPassword`,
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
	if !strings.Contains(source, "m.users.ConfirmPassword(") {
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

	doLogin := bodyOf(t, handlers, "doLogin")
	if !strings.Contains(handlers, `form:"remember"`) ||
		!strings.Contains(doLogin, "remember := in.Remember && !m.withoutRemember") ||
		!strings.Contains(doLogin, "m.finishSignIn(w, r, u, remember)") ||
		!strings.Contains(doLogin, "m.writePending(w, u, remember)") {
		t.Fatal("nothing reads the remember-me box, or it is honoured when the application withheld it")
	}
	if !strings.Contains(handlers, "session.Remember(remember)") {
		t.Error("the answer is read and not passed to the session, so the session still lives for the plain ttl")
	}
	// A rejection is answered by the router, which keeps what was typed in the
	// flash; the box is markup rather than a component, so the screen reads it
	// back from there itself.
	showLogin := bodyOf(t, handlers, "showLogin")
	if !strings.Contains(showLogin, `hhttp.StateFrom(r.Context()).Old.Get("remember") != ""`) {
		t.Error("a rejected sign-in loses the box, and nothing on screen says it was unticked")
	}
	// WithRemember is one value read by both ends: the screen leaves the box
	// off and the handler ignores a field nobody could tick.
	if !strings.Contains(showLogin, "WithoutRemember: m.withoutRemember") {
		t.Error("the sign-in screen draws the box whether or not the application offers it")
	}
	if !strings.Contains(authFile(t, "login.kyse.go"), "@if(.OffersRemember())") {
		t.Error("the sign-in screen draws the remember-me box without asking whether it is offered")
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
// application name on the request, and the mail views name that field --
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
		body := authFile(t, view)
		if !strings.Contains(body, "Brand:") || !strings.Contains(body, ".BrandName") {
			t.Errorf("%s does not draw the brand from the message it renders, so whatever name it shows "+
				"is one this file decided for every project", view)
		}
	}
	for _, mailable := range []string{"VerifyEmail.go", "PasswordReset.go"} {
		if !strings.Contains(authFile(t, mailable), "BrandName string") {
			t.Errorf("%s carries no BrandName, so the view has nothing to draw but a literal", mailable)
		}
	}
	if body := bodyOf(t, authFile(t, "RegisterController.go"), "sendVerification"); !strings.Contains(body, "BrandName: hhttp.AppNameFrom(ctx)") {
		t.Error("the verification mail goes out without the application's name: the field exists and nobody fills it")
	}
	if body := bodyOf(t, authFile(t, "PasswordController.go"), "sendPasswordReset"); !strings.Contains(body, "BrandName: hhttp.AppNameFrom(ctx)") {
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

// TestARejectedFormIsReturnedToTheRouter.
//
// A handler that refuses a form returns validation.Errors, and the router
// answers: a page is sent back where it came from with the messages and what
// was typed, a client that asked for JSON gets a 422 problem document. The kit
// used to draw the refusal itself, with a 422 and the form, and htmx throws a
// 422 away unless the layout teaches it otherwise -- which is the second way of
// answering one rejection, and a reload of that answer posted the form again.
//
// So no published Go writes a refusal status, and the screens are drawn at one
// status only. TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy holds the
// other half: the routes that take a form are registered as actions, which is
// what gives a returned error somewhere to go.
func TestARejectedFormIsReturnedToTheRouter(t *testing.T) {
	var read int
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		read++
		for _, status := range []string{
			"http.StatusUnprocessableEntity", "http.StatusUnauthorized", "http.StatusTooManyRequests",
			"m.screenStatus(", "m.rejected",
		} {
			if strings.Contains(string(f.Content), status) {
				t.Errorf("%s draws a refusal itself (%s): return validation.Errors and let the router send "+
					"the person back to the form", path, status)
			}
		}
	}
	if read == 0 {
		t.Fatal("the kit published no Go, so this gate read nothing")
	}

	render := bodyOf(t, authFile(t, "render.go"), "screen")
	if !strings.Contains(render, "http.StatusOK, name, data)") {
		t.Error("Module.screen no longer renders at 200: a screen this kit draws is never a refusal")
	}
}

// TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn.
//
// A handler computed "Your password has been changed. Sign in with it.", passed
// it to a view with no block to draw it, and the string was thrown away. Nothing
// failed: the screen rendered, the password really had changed, and the person
// was shown an ordinary sign-in form with no reason to believe any of it had
// worked. The same hole put somebody who typed a dead code on the cheerful
// "check your inbox" page.
//
// Two kinds of sentence reach a screen, and each is followed end to end. A
// notice travels as Status, through Module.notify, and some screen has to draw
// .Status. A refusal travels as validation.Errors keyed by the name of an
// input, and the components ask the page for it by that name -- so every key a
// published handler rejects with has to be the name of an input some screen
// draws, or the message is carried back and dropped.
func TestEveryMessageAScreenIsGivenHasSomewhereToBeDrawn(t *testing.T) {
	var markup strings.Builder
	keys := map[string]string{}
	var notified bool
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if strings.HasSuffix(path, ".kyse.go") {
			markup.WriteString(viewBody(f.Content))
			continue
		}
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		notified = notified || strings.Contains(string(f.Content), "m.notify(")
		for _, key := range rejectedFields(t, path, f.Content) {
			keys[key] = path
		}
	}

	if !notified {
		t.Error("no handler leaves a notice with Module.notify, so this half of the gate read nothing")
	}
	if !slices.Contains(fieldsRead(markup.String()), "Status") {
		t.Error("a handler leaves a notice and no screen draws .Status: the sentence is computed, passed and " +
			"thrown away, and the person is told nothing")
	}

	if len(keys) == 0 {
		t.Fatal("no published handler rejects a field, so this half of the gate read nothing")
	}
	for key, path := range keys {
		if strings.Contains(markup.String(), `Name: "`+key+`"`) ||
			strings.Contains(markup.String(), `FieldError("`+key+`")`) {
			continue
		}
		t.Errorf("%s rejects the field %q and no screen has an input by that name: the message goes back "+
			"in the flash and nothing draws it", path, key)
	}
}

// rejectedFields returns the keys a Go file rejects a form with: the string
// keys of every validation.Errors literal, and every errs["key"] it assigns.
func rejectedFields(t *testing.T, path string, content []byte) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filepath.Base(path), content, parser.AllErrors)
	if err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	var out []string
	add := func(expr ast.Expr) {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if key, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, key)
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			if types.ExprString(node.Type) != "validation.Errors" {
				return true
			}
			for _, elt := range node.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					add(kv.Key)
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if index, ok := lhs.(*ast.IndexExpr); ok && types.ExprString(index.X) == "errs" {
					add(index.Index)
				}
			}
		}
		return true
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
	body := bodyOf(t, authFile(t, "LoginController_handlers.go"), "finishSignIn")

	if !strings.Contains(body, `m.sessions.TakeIntended(w, r, "/")`) {
		t.Error("the final sign-in seam does not spend the address the guard remembered.\n" +
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
// the thirteen screens. Two of them are only ever reached WITH a session:
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
// AuthPage carries authenticator provisioning material and the session's CSRF
// token through the view.Page it embeds. ChromeProps carries that same CSRF
// token. Both are ordinary structs a handler holds for the length of a request,
// and the debug page prints a recorded value with json.MarshalIndent, so one
// observability.Dump must not publish a secret somebody can spend.
//
// It is proved by running the published bytes and not by reading them: the
// question is what json.Marshal and slog actually produce, and a method that
// names the right fields and copies the wrong ones reads correct. The kit is
// compiled in a module of its own against the checkouts beside this one, and
// skipped where they are absent -- this module is released alone, and its CI has
// only itself.
func TestNeitherTokenSurvivesBeingSerialized(t *testing.T) {
	const (
		secret   = "authenticator-secret-read-off-a-dump"
		qr       = "svg-provisioning-material"
		recovery = "single-use-recovery-codes"
		csrf     = "csrf-token-read-off-a-screenshot"
	)

	out := runInPublishedKit(t, []string{"encoding/json", "log/slog", "os"}, `
	page := authui.AuthPage{
		SecretKey: `+strconv.Quote(secret)+`,
		QRCodeSVG: `+strconv.Quote(qr)+`,
		RecoveryCodesText: `+strconv.Quote(recovery)+`,
	}
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

	for _, secret := range []string{secret, qr, recovery, csrf} {
		if strings.Contains(out, secret) {
			t.Errorf("a token reaches the output of the published kit:\n%s", out)
		}
	}

	for _, key := range []string{"SecretKey", "QRCodeSVG", "RecoveryCodesText"} {
		if strings.Contains(out, key) {
			t.Errorf("provisioning field %s is present in serialized output:\n%s", key, out)
		}
	}
	if want := `"Token":"[redacted]"`; !strings.Contains(out, want) {
		t.Errorf("the CSRF token is not reported as %s; the page serialized to:\n%s", want, out)
	}
}

// registration is one route the published Routes method mounts: how it is
// reached, the name a URL can be built from, and whether its handler is a
// controller action. An empty name is a screen no template can link to without
// writing the address out.
//
// action is the half that says who answers a rejection. A handler registered
// with Action returns validation.Errors and the router sends the person back to
// the form; one registered with Get or Post has nobody to return it to, so it
// would have to draw the refusal itself.
type registration struct {
	method, path, name string
	action             bool
}

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
	// The method an Action is registered for, spelled the way net/http spells
	// it and read back into the verb the table uses.
	methods := map[string]string{
		"MethodGet": "Get", "MethodPost": "Post", "MethodPut": "Put",
		"MethodPatch": "Patch", "MethodDelete": "Delete",
	}

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
		if !ok {
			continue
		}
		switch {
		case verbs[sel.Sel.Name]:
			out = append(out, registration{
				method: sel.Sel.Name,
				path:   stringLiteral(t, call.Args[0]),
				name:   name,
			})
		case sel.Sel.Name == "Action":
			constant, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok || methods[constant.Sel.Name] == "" {
				t.Fatalf("an Action is registered for %s, which is not a net/http method constant",
					types.ExprString(call.Args[0]))
			}
			out = append(out, registration{
				method: methods[constant.Sel.Name],
				path:   stringLiteral(t, call.Args[1]),
				name:   name,
				action: true,
			})
		}
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
// The application-owned module preserves the established route-name contract.
// An address unchanged but a name gone still breaks every template that builds
// its URL by name.
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
//
// The last column is exact too. The ten that read a form are controller
// actions: each converts its form with ctx.Bind, which only an action is handed
// a context for, and a refusal is returned to the router and answered by it. A
// route that moved back to Post would have to read its form by hand and draw
// its own refusal, which are the second ways of doing both that the kit gave up.
func TestEveryScreenTheKitMountsCarriesTheNameItIsLinkedBy(t *testing.T) {
	want := []registration{
		{"Get", "/login", "auth.login", false},
		{"Post", "/login", "", true},
		{"Post", "/logout", "auth.logout", false},

		{"Get", "/password", "auth.password.request", false},
		{"Post", "/password/email", "auth.password.email", true},
		{"Get", "/password/reset", "auth.password.reset", false},
		{"Post", "/password/update", "auth.password.update", true},
		{"Get", "/password/confirm", "auth.password.confirm", false},
		{"Post", "/password/confirm", "", true},

		{"Get", "/register", "auth.register", false},
		{"Post", "/register", "", true},
		{"Get", "/verify", "auth.verify.notice", false},
		{"Post", "/verify/confirm", "auth.verify.confirm", true},
		{"Post", "/verify/resend", "auth.verify.resend", true},

		{"Get", "/two-factor/challenge", "auth.two-factor.challenge", false},
		{"Post", "/two-factor/challenge", "", true},
		{"Get", "/two-factor/recovery", "auth.two-factor.recovery", false},
		{"Post", "/two-factor/recovery", "", true},
		{"Get", "/two-factor/setup", "auth.two-factor.setup", false},
		{"Post", "/two-factor/setup", "", false},
		{"Post", "/two-factor/setup/confirm", "auth.two-factor.setup.confirm", true},
		{"Post", "/two-factor/disable", "auth.two-factor.disable", false},
		{"Post", "/two-factor/recovery-codes", "auth.two-factor.recovery-codes", false},
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
			if route.name != want[i].name {
				t.Errorf("%s %s is named %q and must be named %q", route.method, route.path, route.name, want[i].name)
			}
			if route.action != want[i].action {
				t.Errorf("%s %s is registered with action=%t and must be action=%t: a handler that refuses a "+
					"form returns validation.Errors to the router, and only Action gives it a router to return them to",
					route.method, route.path, route.action, want[i].action)
			}
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
	}, `
	published := http.NewRouter()

	// Nothing here serves a request, so the zero value is the module: Routes
	// reads one field of it, and only to hand the session store to two guards.
	(&authui.Module{}).Routes(published)

	for _, name := range []string{
		"auth.login", "auth.logout",
		"auth.password.request", "auth.password.email", "auth.password.reset",
		"auth.password.update", "auth.password.confirm",
		"auth.register", "auth.verify.notice", "auth.verify.confirm", "auth.verify.resend",
		"auth.two-factor.challenge", "auth.two-factor.recovery", "auth.two-factor.setup",
		"auth.two-factor.setup.confirm", "auth.two-factor.disable", "auth.two-factor.recovery-codes",
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
		"auth.login=/auth/login",
		"auth.logout=/auth/logout",
		"auth.password.request=/auth/password",
		"auth.password.email=/auth/password/email",
		"auth.password.reset=/auth/password/reset",
		"auth.password.update=/auth/password/update",
		"auth.password.confirm=/auth/password/confirm",
		"auth.register=/auth/register",
		"auth.verify.notice=/auth/verify",
		"auth.verify.confirm=/auth/verify/confirm",
		"auth.verify.resend=/auth/verify/resend",
		"auth.two-factor.challenge=/auth/two-factor/challenge",
		"auth.two-factor.recovery=/auth/two-factor/recovery",
		"auth.two-factor.setup=/auth/two-factor/setup",
		"auth.two-factor.setup.confirm=/auth/two-factor/setup/confirm",
		"auth.two-factor.disable=/auth/two-factor/disable",
		"auth.two-factor.recovery-codes=/auth/two-factor/recovery-codes",
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
	writeInto(t, filepath.Join(root, "app", "Models", "User.go"), []byte(publishedUserModel))

	gomod := "module " + authSpec().ModulePath + "\n\ngo 1.26\n\nrequire (\n" +
		"\tgithub.com/arandu-io/framework v0.0.0\n\tgithub.com/arandu-io/hesape v0.0.0\n\tgithub.com/arandu-io/kyse v0.0.0\n)\n\n"
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

// TestALockedChallengeSendsThePersonBackToSignIn runs the published two-factor
// handlers against an account whose challenge budget is spent.
//
// The application's service answers that with an error carrying Seconds() that
// also matches twofactor.ErrInvalidCode, so a handler that only knew about wrong
// codes would send the person back to the challenge, to be refused again on
// every try with the pending cookie still live. Both challenge screens must
// instead end the pending sign-in, say how long the account waits, and send the
// person to sign in, where the reason is drawn. A plain wrong code must still go
// back to the challenge it was typed on, with the pending sign-in intact.
func TestALockedChallengeSendsThePersonBackToSignIn(t *testing.T) {
	out := runAgainstPublishedKit(t, lockedChallengeProbe)

	for _, want := range []string{
		"login status=303",
		"challenge: wrong status=303 location=/auth/two-factor/challenge pending-cleared=false",
		"challenge: locked status=303 location=/auth/login retry-after=900 pending-cleared=true",
		"recovery: wrong status=303 location=/auth/two-factor/recovery pending-cleared=false",
		"recovery: locked status=303 location=/auth/login retry-after=900 pending-cleared=true",
		"sign-in screen: Too many codes. Sign in again in 15 minutes.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// lockedChallengeProbe drives the published module through its own router: a
// password sign-in that starts a challenge, then each challenge screen offered a
// wrong code and a code the account may no longer offer.
//
// The router is wrapped in middleware.Flash, the piece the kernel installs above
// every application, because that is what puts a redirect's flash on the page
// that follows it.
const lockedChallengeProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/hesape/auth/account"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

// locked is what the application's service returns once the account has
// offered too many codes: it matches the invalid-code sentinel as well.
type locked struct{}

func (locked) Error() string { return "two-factor: too many codes" }
func (locked) Seconds() int { return 900 }
func (locked) Unwrap() error { return twofactor.ErrInvalidCode }

var user = &models.User{ID: "user-a", TenantID: "tenant-a", Email: "a@example.test", Password: "secret"}

type users struct{ authui.Users }

func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return user, nil
}

func (users) FindForAuthentication(context.Context, string, string) (account.Account, error) {
	return user, nil
}

type factors struct {
	authui.Factors
	err error
}

func (factors) Required(context.Context, string, string) (bool, error) { return true, nil }

func (f *factors) VerifyAuthenticator(context.Context, string, string, string) error { return f.err }

func (f *factors) ConsumeRecovery(context.Context, string, string, string) error { return f.err }

func main() {
	for _, name := range []string{"auth.login", "auth.two-factor.challenge", "auth.two-factor.recovery"} {
		view.Register(name, func(w io.Writer, data any) error {
			_, err := io.WriteString(w, data.(authui.AuthPage).Status)
			return err
		})
	}

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	f := &factors{}
	module := authui.New(users{}, f, nil, sessions, session.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	post := func(path string, form url.Values, cookies []*http.Cookie) *http.Response {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// The page the form was on, as a browser sends it: it is where a
		// refused form is sent back to.
		r.Header.Set("Referer", path)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w.Result()
	}
	cleared := func(res *http.Response) bool {
		for _, c := range res.Cookies() {
			if c.Name == "two-factor-pending" && c.MaxAge < 0 {
				return true
			}
		}
		return false
	}

	signIn := post("/auth/login", url.Values{"email": {user.Email}, "password": {user.Password}}, nil)
	fmt.Printf("login status=%d\n", signIn.StatusCode)
	pending := signIn.Cookies()

	var flash []*http.Cookie
	for _, screen := range []struct{ name, path, field string }{
		{"challenge", "/auth/two-factor/challenge", "authenticator_code"},
		{"recovery", "/auth/two-factor/recovery", "recovery_code"},
	} {
		f.err = fmt.Errorf("wrapped: %w", twofactor.ErrInvalidCode)
		res := post(screen.path, url.Values{screen.field: {"123456"}}, pending)
		fmt.Printf("%s: wrong status=%d location=%s pending-cleared=%t\n", screen.name,
			res.StatusCode, res.Header.Get("Location"), cleared(res))

		f.err = fmt.Errorf("wrapped: %w", locked{})
		res = post(screen.path, url.Values{screen.field: {"123456"}}, pending)
		fmt.Printf("%s: locked status=%d location=%s retry-after=%s pending-cleared=%t\n", screen.name,
			res.StatusCode, res.Header.Get("Location"), res.Header.Get("Retry-After"), cleared(res))
		flash = res.Cookies()
	}
	if !errors.Is(locked{}, twofactor.ErrInvalidCode) {
		panic("the probe's lock does not match the invalid-code sentinel, so it proves nothing")
	}

	r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	r.Header.Set("Accept", "text/html")
	for _, c := range flash {
		if c.MaxAge >= 0 {
			r.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	fmt.Printf("sign-in screen: %s\n", w.Body.String())
}
`

// TestTheSignedInScreensActForTheSubjectTheGuardLoaded runs the published
// handlers behind RequireAuth and RequireConfirmedPassword.
//
// Those handlers read the subject from the request context, where the guards
// put the one they loaded, instead of loading the session a second time. A
// framework whose guards dropped it would send every signed-in person back to
// sign in from the password confirmation and the two-factor settings, so this
// asks the published module, through its own router, who each one acted for.
func TestTheSignedInScreensActForTheSubjectTheGuardLoaded(t *testing.T) {
	out := runAgainstPublishedKit(t, signedInProbe)

	for _, want := range []string{
		"guest confirm: status=303 location=/auth/login",
		"confirm: status=303 acted-for=tenant-a/user-a",
		"disable: status=303 location=/ acted-for=tenant-a/user-a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// signedInProbe signs a session in, confirms its password and disables its
// second factor, recording the subject each fake service was handed.
const signedInProbe = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"

	authui "example.test/project/app/Http/Controllers/Auth"
)

var actedFor string

func record(s nativeauth.Subject) { actedFor = s.Tenant + "/" + s.ID }

type users struct{ authui.Users }

func (users) ConfirmPassword(_ context.Context, s nativeauth.Subject, _, _ string) error {
	record(s)
	return nil
}

type factors struct{ authui.Factors }

func (factors) Disable(_ context.Context, s nativeauth.Subject) error {
	record(s)
	return nil
}

func main() {
	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	module := authui.New(users{}, factors{}, nil, sessions, security.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)

	signedIn := httptest.NewRecorder()
	if _, err := sessions.Rotate(context.Background(), signedIn, "",
		security.Subject{ID: "user-a", Tenant: "tenant-a"}, security.Remember(false)); err != nil {
		panic(err)
	}
	cookies := signedIn.Result().Cookies()

	post := func(path string, form url.Values, cookies []*http.Cookie) *http.Response {
		actedFor = ""
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Result()
	}

	res := post("/auth/password/confirm", url.Values{"password": {"secret"}}, nil)
	fmt.Printf("guest confirm: status=%d location=%s\n", res.StatusCode, res.Header.Get("Location"))

	res = post("/auth/password/confirm", url.Values{"password": {"secret"}}, cookies)
	fmt.Printf("confirm: status=%d acted-for=%s\n", res.StatusCode, actedFor)
	for _, c := range res.Cookies() {
		for i := range cookies {
			if cookies[i].Name == c.Name {
				cookies[i] = c
			}
		}
	}

	res = post("/auth/two-factor/disable", nil, cookies)
	fmt.Printf("disable: status=%d location=%s acted-for=%s\n", res.StatusCode, res.Header.Get("Location"), actedFor)
}
`

// TestAGuestScreenCarriesTheTokenCSRFProtectIssued runs the published sign-in
// screen behind the framework's CSRFProtect, the way an application mounts it.
//
// A visitor with no session is bound to a guest cookie, and the issuer refuses
// a token bound to nothing, so a screen that issued its own token from the
// session id answered 500 to every guest. The screen draws the token the
// middleware put on the request context instead; posting it back with the guest
// cookie has to pass the check, and the screen the refusal sends the person back
// to has to carry a token that passes it again.
func TestAGuestScreenCarriesTheTokenCSRFProtectIssued(t *testing.T) {
	out := runAgainstPublishedKit(t, guestTokenProbe)

	for _, want := range []string{
		"get: status=200 token=true guest-cookie=true",
		"post: status=303 location=/auth/login",
		"back: status=200 token=true message=invalid email or password",
		"again: status=303 location=/auth/login",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// guestTokenProbe loads the sign-in screen as a guest, submits it with a
// password the fake account service refuses, follows the redirect back, and
// submits the form it lands on.
const guestTokenProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/hesape/auth/account"

	authui "example.test/project/app/Http/Controllers/Auth"
)

type users struct{ authui.Users }

func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return nil, nativeauth.ErrInvalidCredentials
}

func main() {
	view.Register("auth.login", func(w io.Writer, data any) error {
		page := data.(authui.AuthPage)
		_, err := io.WriteString(w, page.Token+"|"+page.FieldError("email"))
		return err
	})

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	csrf := session.NewCSRF(appKey, time.Hour)
	module := authui.New(users{}, nil, nil, sessions, csrf, nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(
		middleware.CSRFProtect(csrf, sessions.IDFromRequest)(router))

	jar := map[string]*http.Cookie{}
	keep := func(res *http.Response) {
		for _, c := range res.Cookies() {
			if c.MaxAge < 0 {
				delete(jar, c.Name)
				continue
			}
			jar[c.Name] = c
		}
	}
	get := func() (int, string, string) {
		r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		r.Header.Set("Accept", "text/html")
		for _, c := range jar {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		keep(w.Result())
		token, message, _ := strings.Cut(w.Body.String(), "|")
		return w.Code, token, message
	}
	post := func(token string) *http.Response {
		form := url.Values{"_token": {token}, "email": {"a@example.test"}, "password": {"wrong"}}
		r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", "/auth/login")
		for _, c := range jar {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		keep(w.Result())
		return w.Result()
	}

	code, token, _ := get()
	fmt.Printf("get: status=%d token=%t guest-cookie=%t\n", code, token != "", len(jar) > 0)

	res := post(token)
	fmt.Printf("post: status=%d location=%s\n", res.StatusCode, res.Header.Get("Location"))

	code, token, message := get()
	fmt.Printf("back: status=%d token=%t message=%s\n", code, token != "", message)

	res = post(token)
	fmt.Printf("again: status=%d location=%s\n", res.StatusCode, res.Header.Get("Location"))
}
`

// TestARejectedSignInGoesBackToTheFormInEveryTransport runs a refused sign-in
// through the published module the three ways it arrives.
//
// A browser posting the form gets a 303 back to the form, marked no-store, with
// the message and the address in the flash and the password nowhere -- and the
// screen it lands on draws them. htmx gets the same redirect as HX-Redirect with
// no body, which it follows as a navigation. A client that asked for JSON gets a
// 422 problem document with the message by field, because it has no form to go
// back to.
//
// A sent reset code is the other half: a screen drawn after a form succeeded is
// reached by a redirect too, so the form on it has an address a refusal can be
// sent back to, and a reload asks for the screen rather than sending another
// code.
func TestARejectedSignInGoesBackToTheFormInEveryTransport(t *testing.T) {
	out := runAgainstPublishedKit(t, transportProbe)

	for _, want := range []string{
		"page: status=303 location=/auth/login cache=no-store, private",
		"page then: message=invalid email or password email=a@example.test password=",
		"htmx: status=204 hx-redirect=/auth/login body=0",
		"json: status=422 type=application/problem+json message=true",
		"send: status=303 location=/auth/password/reset",
		"reset then: status=If that address is registered, a code is on its way. email=a@example.test",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// transportProbe posts one refused sign-in as a page, as htmx and as JSON, and
// asks for a reset code.
const transportProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/hesape/auth/account"

	authui "example.test/project/app/Http/Controllers/Auth"
)

type users struct{ authui.Users }

func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return nil, nativeauth.ErrInvalidCredentials
}

// Lookup finds nobody, so nothing is mailed and the answer is the one an
// address nobody registered gets.
func (users) Lookup(context.Context, string, string) (account.Account, error) {
	return nil, errors.New("no such account")
}

func main() {
	view.Register("auth.login", func(w io.Writer, data any) error {
		page := data.(authui.AuthPage)
		_, err := fmt.Fprintf(w, "message=%s email=%s password=%s",
			page.FieldError("email"), page.OldOr("email", ""), page.OldOr("password", ""))
		return err
	})
	view.Register("auth.passwords.reset", func(w io.Writer, data any) error {
		page := data.(authui.AuthPage)
		_, err := fmt.Fprintf(w, "status=%s email=%s", page.Status, page.OldOr("email", ""))
		return err
	})

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	module := authui.New(users{}, nil, nil, sessions, session.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	post := func(path string, form url.Values, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", path)
		for k, v := range header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	follow := func(w *httptest.ResponseRecorder) string {
		if w.Header().Get("Location") == "" {
			return "(no redirect to follow)"
		}
		r := httptest.NewRequest(http.MethodGet, w.Header().Get("Location"), nil)
		r.Header.Set("Accept", "text/html")
		for _, c := range w.Result().Cookies() {
			if c.MaxAge >= 0 {
				r.AddCookie(c)
			}
		}
		next := httptest.NewRecorder()
		app.ServeHTTP(next, r)
		return next.Body.String()
	}
	refused := url.Values{"email": {"a@example.test"}, "password": {"wrong-password"}}

	page := post("/auth/login", refused, map[string]string{"Accept": "text/html"})
	fmt.Printf("page: status=%d location=%s cache=%s\n", page.Code, page.Header().Get("Location"),
		page.Header().Get("Cache-Control"))
	fmt.Printf("page then: %s\n", follow(page))

	htmx := post("/auth/login", refused, map[string]string{"HX-Request": "true"})
	fmt.Printf("htmx: status=%d hx-redirect=%s body=%d\n", htmx.Code, htmx.Header().Get("HX-Redirect"),
		htmx.Body.Len())

	json := post("/auth/login", refused, map[string]string{"Accept": "application/json"})
	fmt.Printf("json: status=%d type=%s message=%t\n", json.Code, json.Header().Get("Content-Type"),
		strings.Contains(json.Body.String(), "invalid email or password"))

	send := post("/auth/password/email", url.Values{"email": {"a@example.test"}}, nil)
	fmt.Printf("send: status=%d location=%s\n", send.Code, send.Header().Get("Location"))
	fmt.Printf("reset then: %s\n", follow(send))
}
`

// TestTheKitWritesItsNoticesWithTheRoutersFlash.
//
// The kernel's router carries the flash the application reads back on the next
// page, with the key and the Secure attribute the application decided once. A
// module that builds a flash of its own repeats that decision and can make it
// differently -- the kit's own was Secure by the skeleton's rule while the
// kernel's was Secure by another -- so Routes takes the router's when it has one.
//
// The probe tells the two apart by key and by attribute. The module is built
// with one key and secure=false, the router's flash with another key and
// secure=true, and the application reads with the router's: a notice written
// with the module's own flash would carry no Secure and would not verify, so the
// reset screen would draw no status. The second half is the fallback: a router
// with no flash, as a test builds one, gets the module's, and the cookie carries
// the secure New was given.
func TestTheKitWritesItsNoticesWithTheRoutersFlash(t *testing.T) {
	out := runAgainstPublishedKit(t, routerFlashProbe)

	for _, want := range []string{
		"router notice: status=303 secure=true",
		"router then: status=If that address is registered, a code is on its way. email=a@example.test",
		"router refusal: status=303 secure=true",
		"router refusal then: message=invalid email or password",
		"fallback notice: status=303 secure=true",
		"fallback then: status=If that address is registered, a code is on its way. email=a@example.test",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// routerFlashProbe posts a reset request and a refused sign-in through a router
// that carries a flash, and a reset request through one that carries none.
const routerFlashProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/hesape/auth/account"

	authui "example.test/project/app/Http/Controllers/Auth"
)

type users struct{ authui.Users }

func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return nil, nativeauth.ErrInvalidCredentials
}

func (users) Lookup(context.Context, string, string) (account.Account, error) {
	return nil, errors.New("no such account")
}

func main() {
	view.Register("auth.login", func(w io.Writer, data any) error {
		_, err := fmt.Fprintf(w, "message=%s", data.(authui.AuthPage).FieldError("email"))
		return err
	})
	view.Register("auth.passwords.reset", func(w io.Writer, data any) error {
		page := data.(authui.AuthPage)
		_, err := fmt.Fprintf(w, "status=%s email=%s", page.Status, page.OldOr("email", ""))
		return err
	})

	moduleKey := []byte("0123456789abcdef0123456789abcdef")
	routerKey := []byte("fedcba9876543210fedcba9876543210")
	build := func(secure bool) *authui.Module {
		sessions := security.NewSessionStore(moduleKey, time.Hour, false,
			security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
		return authui.New(users{}, nil, nil, sessions, session.NewCSRF(moduleKey, time.Hour), nil,
			nil, moduleKey, authui.FixedTenant("tenant-a"), secure)
	}

	post := func(app http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", path)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	secure := func(w *httptest.ResponseRecorder) string {
		for _, c := range w.Result().Cookies() {
			if c.Name == session.FlashCookieName && c.MaxAge >= 0 {
				return fmt.Sprint(c.Secure)
			}
		}
		return "(no flash cookie)"
	}
	follow := func(app http.Handler, w *httptest.ResponseRecorder) string {
		r := httptest.NewRequest(http.MethodGet, w.Header().Get("Location"), nil)
		r.Header.Set("Accept", "text/html")
		for _, c := range w.Result().Cookies() {
			if c.MaxAge >= 0 {
				r.AddCookie(c)
			}
		}
		next := httptest.NewRecorder()
		app.ServeHTTP(next, r)
		return next.Body.String()
	}
	reset := url.Values{"email": {"a@example.test"}}
	refused := url.Values{"email": {"a@example.test"}, "password": {"wrong-password"}}

	routerFlash := session.NewFlash(routerKey, true)
	router := fhttp.NewRouter().WithFlash(routerFlash)
	build(false).Routes(router)
	app := middleware.Flash(routerFlash)(router)

	notice := post(app, "/auth/password/email", reset)
	fmt.Printf("router notice: status=%d secure=%s\n", notice.Code, secure(notice))
	fmt.Printf("router then: %s\n", follow(app, notice))
	refusal := post(app, "/auth/login", refused)
	fmt.Printf("router refusal: status=%d secure=%s\n", refusal.Code, secure(refusal))
	fmt.Printf("router refusal then: %s\n", follow(app, refusal))

	bare := fhttp.NewRouter()
	build(true).Routes(bare)
	fallback := middleware.Flash(session.NewFlash(moduleKey, false))(bare)
	own := post(fallback, "/auth/password/email", reset)
	fmt.Printf("fallback notice: status=%d secure=%s\n", own.Code, secure(own))
	fmt.Printf("fallback then: %s\n", follow(fallback, own))
}
`

// TestAPasswordStoredTrimmedSignsInWithoutBeingRewritten.
//
// ctx.Bind trimmed the password until hesape v0.50.2, so the kit's v0.21.0
// handlers stored a password chosen with spaces at its ends without them. Bind
// now leaves it as typed, and the person typing what they chose would be
// refused. The published sign-in asks the user service a second time with the
// trimmed form, and writes nothing.
//
// It used to store the password as typed on success, through ResetPassword.
// That rewrote the hash of anybody whose keyboard added a space at the end, so
// the next sign-in without the space was refused and a PasswordReset event was
// stored for a change nobody asked for; and ResetPassword refused an account
// Hooks.Admit refuses as a spent link, which the handler answered with a 500
// where a wrong password answers 422 -- an oracle for a refused account's
// password.
//
// The probe runs the published module with a user service that hashes for
// real and counts what the real one counts: each call to VerifyCredentials is
// one attempt against the throttle.
//
//   - typed with the spaces: signed in, twice in a row, and without the spaces
//     too, with the stored hash untouched and nothing reset;
//   - typed with the spaces, the second factor on: the challenge opens, as it
//     does for the password typed exactly;
//   - wrong, unknown, an account with no password, an account Admit refuses
//     and a service that fails on the second form: the usual message, two
//     attempts, nothing written;
//   - no spaces, only spaces, or the account held back: no second form at all.
//
// The lock in the probe matches ErrInvalidCredentials as well, the shape
// twofactor's lock has, because that is the one a second form could mistake
// for a wrong password. What this holds is that it is never retried.
func TestAPasswordStoredTrimmedSignsInWithoutBeingRewritten(t *testing.T) {
	out := runAgainstPublishedKit(t, trimmedPasswordProbe)

	for _, want := range []string{
		"spaces: status=303 location=/ attempts=2 resets=0 unchanged=true",
		"again: status=303 location=/ attempts=2 resets=0 unchanged=true",
		"without the spaces: status=303 location=/ attempts=1 resets=0 unchanged=true",
		"second factor: status=303 location=/auth/two-factor/challenge attempts=2 resets=0 challenge=200",
		"exact, second factor: status=303 location=/auth/two-factor/challenge attempts=1 resets=0 challenge=200",
		"wrong: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"unknown: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"no password: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"refused: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"refused, wrong: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"failing: status=303 location=/auth/login attempts=2 resets=0 message=invalid email or password",
		"no spaces: status=303 location=/auth/login attempts=1 resets=0 message=invalid email or password",
		"only spaces: status=303 location=/auth/login attempts=1 resets=0 message=invalid email or password",
		"locked: status=303 location=/auth/login attempts=1 resets=0 message=invalid email or password",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}

	// The second form is the user service's own check, so the throttle, the
	// timebox and Hooks.Admit hold it as they hold the first; and nothing in
	// the transition writes.
	body := bodyOf(t, authFile(t, "LoginController_handlers.go"), "verifyTrimmedPassword")
	if !strings.Contains(body, "m.users.VerifyCredentials(") {
		t.Error("verifyTrimmedPassword no longer asks the user service, so its throttle, its timebox and Admit no longer hold the second form")
	}
	for _, gone := range []string{"ResetPassword", "SetPassword", "ChangePassword", "hashing."} {
		if strings.Contains(body, gone) {
			t.Errorf("verifyTrimmedPassword names %s: a sign-in through it rewrites the password or compares it by hand", gone)
		}
	}
}

// trimmedPasswordProbe signs in through the published module, with a user
// service that keeps real hashes.
const trimmedPasswordProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/hashing"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/hesape/auth/account"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

const (
	typed   = "  correct horse battery staple  "
	trimmed = "correct horse battery staple"
)

type locked struct{}

func (locked) Error() string { return "too many attempts" }
func (locked) Seconds() int { return 60 }
func (locked) Unwrap() error { return nativeauth.ErrInvalidCredentials }

type users struct {
	authui.Users
	accounts         map[string]*models.User
	attempts, resets int
	held             string
	// refused is an address Hooks.Admit refuses, answered as the real
	// service answers it; failing is one whose service fails on a right
	// password.
	refused, failing string
}

func (u *users) VerifyCredentials(_ context.Context, _, email, password, _ string) (account.Account, error) {
	u.attempts++
	if email == u.held {
		return nil, locked{}
	}
	acct, ok := u.accounts[email]
	if !ok || acct.Password == "" || hashing.Check(password, acct.Password) != nil {
		return nil, nativeauth.ErrInvalidCredentials
	}
	switch email {
	case u.refused:
		return nil, account.ErrAccountRefused
	case u.failing:
		return nil, errors.New("the database went away")
	}
	return acct, nil
}

func (u *users) FindForAuthentication(_ context.Context, _, id string) (account.Account, error) {
	for _, acct := range u.accounts {
		if acct.ID == id {
			return acct, nil
		}
	}
	return nil, errors.New("no such account")
}

func (u *users) ResetPassword(_ context.Context, _, id, email, fingerprint, password string) (account.Account, error) {
	u.resets++
	acct := u.accounts[email]
	if acct == nil || acct.ID != id || account.Fingerprint(acct.Password) != fingerprint {
		return nil, errors.New("the password changed")
	}
	hash, err := hashing.Make(password)
	if err != nil {
		return nil, err
	}
	acct.Password = hash
	return acct, nil
}

type factors struct{ authui.Factors }

func (factors) Required(_ context.Context, _, id string) (bool, error) { return id == "user-c", nil }

func main() {
	view.Register("auth.login", func(w io.Writer, data any) error {
		_, err := io.WriteString(w, data.(authui.AuthPage).FieldError("email"))
		return err
	})
	view.Register("auth.two-factor.challenge", func(w io.Writer, data any) error {
		_, err := io.WriteString(w, "challenge")
		return err
	})

	hash, err := hashing.Make(trimmed)
	if err != nil {
		panic(err)
	}
	service := &users{accounts: map[string]*models.User{
		"a@example.test": {ID: "user-a", TenantID: "tenant-a", Email: "a@example.test", Password: hash},
		"c@example.test": {ID: "user-c", TenantID: "tenant-a", Email: "c@example.test", Password: hash},
		"n@example.test": {ID: "user-n", TenantID: "tenant-a", Email: "n@example.test"},
		"r@example.test": {ID: "user-r", TenantID: "tenant-a", Email: "r@example.test", Password: hash},
		"f@example.test": {ID: "user-f", TenantID: "tenant-a", Email: "f@example.test", Password: hash},
	}, held: "h@example.test", refused: "r@example.test", failing: "f@example.test"}

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	flash := session.NewFlash(appKey, false)
	router := fhttp.NewRouter().WithFlash(flash)
	authui.New(service, factors{}, nil, sessions, session.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false).Routes(router)
	app := middleware.Flash(flash)(router)

	send := func(r *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
		r.Header.Set("Accept", "text/html")
		for _, c := range cookies {
			if c.MaxAge >= 0 {
				r.AddCookie(c)
			}
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	signIn := func(email, password string) *httptest.ResponseRecorder {
		form := url.Values{"email": {email}, "password": {password}}
		r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", "/auth/login")
		return send(r, nil)
	}
	counted := func() string {
		line := fmt.Sprintf("attempts=%d resets=%d", service.attempts, service.resets)
		service.attempts, service.resets = 0, 0
		return line
	}
	signedIn := func(name, email, password string) {
		w := signIn(email, password)
		fmt.Printf("%s: status=%d location=%s %s unchanged=%t\n", name, w.Code, w.Header().Get("Location"),
			counted(), service.accounts[email].Password == hash)
	}
	challenged := func(name, email, password string) {
		w := signIn(email, password)
		challenge := send(httptest.NewRequest(http.MethodGet, "/auth/two-factor/challenge", nil), w.Result().Cookies())
		fmt.Printf("%s: status=%d location=%s %s challenge=%d\n", name, w.Code, w.Header().Get("Location"),
			counted(), challenge.Code)
	}
	refused := func(name, email, password string) {
		w := signIn(email, password)
		line := fmt.Sprintf("%s: status=%d location=%s %s", name, w.Code, w.Header().Get("Location"), counted())
		if to := w.Header().Get("Location"); to != "" {
			page := send(httptest.NewRequest(http.MethodGet, to, nil), w.Result().Cookies())
			line += " message=" + page.Body.String()
		} else {
			line += " message=(no redirect to follow)"
		}
		fmt.Println(line)
	}

	signedIn("spaces", "a@example.test", typed)
	signedIn("again", "a@example.test", typed)
	signedIn("without the spaces", "a@example.test", trimmed)

	challenged("second factor", "c@example.test", typed)
	challenged("exact, second factor", "c@example.test", trimmed)

	refused("wrong", "c@example.test", "  wrong horse battery staple  ")
	refused("unknown", "b@example.test", typed)
	refused("no password", "n@example.test", typed)
	refused("refused", "r@example.test", typed)
	refused("refused, wrong", "r@example.test", "  wrong horse battery staple  ")
	refused("failing", "f@example.test", typed)
	refused("no spaces", "c@example.test", "wrong horse battery staple")
	refused("only spaces", "c@example.test", "              ")
	refused("locked", "h@example.test", typed)
}
`

// TestNoPublishedHandlerReadsItsFormByHand.
//
// Every handler that reads a form converts it with ctx.Bind into a request
// struct declared beside it, and none reads a field by hand: a field read in
// the body is a second request type nobody else can share, and one that is
// missing arrives as an empty string rather than a message. The doctor reports
// both shapes in a project as input-read-by-hand, and it reported ten of them
// on every project this kit was published into. Nor does a handler call
// Validate(): the checks a form here needs are written in the handler, where
// the kit's contract keeps them.
//
// The second half reads the routes: each of the ten registered with Action is
// a handler that binds, so a form added to the kit arrives bound or not at all.
func TestNoPublishedHandlerReadsItsFormByHand(t *testing.T) {
	sources := map[string]string{}
	for _, f := range mustGenerateAuth(t) {
		path := filepath.ToSlash(f.Path)
		if !strings.HasPrefix(path, "app/Http/Controllers/") || strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		sources[path] = string(f.Content)
		for _, byHand := range []string{
			".PostFormValue(", ".FormValue(", ".ParseForm(", ".ParseMultipartForm(", "json.NewDecoder(",
			"ctx.Input(", ".Validate()",
		} {
			if strings.Contains(string(f.Content), byHand) {
				t.Errorf("%s calls %s: a form is converted with ctx.Bind into a request struct, and the "+
					"handler neither reads a field by hand nor calls Validate", path, byHand)
			}
		}
	}

	routes := bodyOf(t, authFile(t, "Auth/LoginController.go"), "Routes")
	actions := regexp.MustCompile(`g\.Action\(stdhttp\.MethodPost, "[^"]+", m\.(\w+)`).FindAllStringSubmatch(routes, -1)
	if len(actions) != 10 {
		t.Fatalf("the module registers %d actions and this test knows 10", len(actions))
	}
	for _, action := range actions {
		handler := action[1]
		var body string
		for _, source := range sources {
			if strings.Contains(source, "func (m *Module) "+handler+"(") {
				body = bodyOf(t, source, handler)
			}
		}
		switch {
		case body == "":
			t.Errorf("no published file declares %s", handler)
		case !strings.Contains(body, "ctx.Bind(&in)"):
			t.Errorf("%s is registered with Action and does not convert its form with ctx.Bind(&in)", handler)
		}
	}
}

// TestTheChecklistAndTheHandlerAreOnePasswordPolicy.
//
// The password box draws its checklist from a policy, and with none it draws
// the application's registered default -- a minimum of eight when nothing was
// registered. The handlers refused anything under hashing.MinPasswordLen,
// twelve. So the sign-up screen ticked every line at eight characters and the
// handler turned the password away: two policies, agreeing on nothing.
//
// One declaration now, passwordPolicy, and both ends read it. The two screens
// where a password is chosen hand it to the box; the handlers that draw them
// fill it; the handlers that take the form check against it. A box that is
// Confirming draws no checklist and needs none.
func TestTheChecklistAndTheHandlerAreOnePasswordPolicy(t *testing.T) {
	register := authFile(t, "RegisterController.go")
	policy := bodyOf(t, register, "passwordPolicy")
	if !strings.Contains(policy, "validation.PasswordMin(hashing.MinPasswordLen).Max(hashing.MaxPasswordLen)") {
		t.Errorf("passwordPolicy does not start from the bounds hashing.Make enforces:\n%s", policy)
	}

	for view, handler := range map[string]struct{ file, show string }{
		"auth/register.kyse.go":        {"RegisterController.go", "showRegister"},
		"auth/passwords/reset.kyse.go": {"PasswordController.go", "showPasswordReset"},
	} {
		markup := authView(t, view)
		for rest := markup; ; {
			at := strings.Index(rest, "components.Password(")
			if at < 0 {
				break
			}
			rest = rest[at+len("components.Password("):]
			call := rest
			if end := strings.Index(call, "}) !!}"); end >= 0 {
				call = call[:end]
			}
			if !strings.Contains(call, "Confirming: true") && !strings.Contains(call, "Policy: .PasswordPolicy") {
				t.Errorf("%s draws a password checklist without the policy its handler checks with", view)
			}
		}
		if !regexp.MustCompile(`PasswordPolicy:\s+passwordPolicy\(\)`).MatchString(bodyOf(t, authFile(t, handler.file), handler.show)) {
			t.Errorf("%s draws %s without filling PasswordPolicy: the checklist falls back to a default the "+
				"handler does not apply", handler.show, view)
		}
	}
	for _, c := range []struct{ file, handler string }{
		{"RegisterController.go", "doRegister"},
		{"PasswordController.go", "updatePassword"},
	} {
		if !strings.Contains(bodyOf(t, authFile(t, c.file), c.handler), `checkNewPassword(errs, "password", in.Password)`) {
			t.Errorf("%s checks a new password against something other than passwordPolicy", c.handler)
		}
	}
}

// TestEveryPublishedPageDrawsTheApplicationNameTheRequestCarries runs the
// published module and landing page twice: once with an application name on
// the request, put there with hhttp.WithAppName as the framework puts
// APP_NAME on every request, and once with none.
//
// The name is one no screen is titled with, so a screen that drew its title
// where the brand goes cannot pass for one that drew the brand. The landing
// page is titled with the name on purpose, and is asked for both. The two
// messages and the issuer an authenticator app files a second factor under are
// read the same way. With no name on the request every one of them is empty:
// nothing published keeps a name of its own, or takes one from a constructor.
func TestEveryPublishedPageDrawsTheApplicationNameTheRequestCarries(t *testing.T) {
	const brand = "Brandproof Ltd"
	out := runAgainstPublishedKit(t, appNameProbe)

	want := []string{
		"screen welcome", "screen home", "screen auth.login", "screen auth.register",
		"screen auth.passwords.email", "screen auth.passwords.reset", "screen auth.passwords.confirm",
		"screen auth.verify", "screen auth.two-factor.challenge", "screen auth.two-factor.recovery",
		"screen auth.two-factor.setup", "screen auth.two-factor.recovery-codes",
		"mail mail.verify-email", "mail mail.verify-email-text",
		"mail mail.password-reset", "mail mail.password-reset-text",
		"issuer two-factor",
	}
	seen := map[string]map[string]bool{"named": {}, "bare": {}}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// The log of the setup the probe stops shares the output; the probe's
		// own lines are the ones that open with the run.
		if !strings.HasPrefix(line, "named|") && !strings.HasPrefix(line, "bare|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 {
			t.Errorf("the probe printed a line it was not written to print: %q", line)
			continue
		}
		run, kind, what, title, got := parts[0], parts[1], parts[2], parts[3], parts[4]
		if kind == "miss" {
			t.Errorf("%s: %s answered %s instead of drawing a screen", run, what, title)
			continue
		}
		seen[run][kind+" "+what] = true

		landing := kind == "screen" && (what == "welcome" || what == "home")
		switch run {
		case "named":
			if got != brand {
				t.Errorf("%s %s drew the brand %q with %q on the request: the application name the framework "+
					"puts there is not what it reads", kind, what, got, brand)
			}
			if kind == "screen" && landing && title != brand {
				t.Errorf("the landing page %s is titled %q, and its title is the application name on the request, %q",
					what, title, brand)
			}
			if kind == "screen" && !landing && (title == "" || title == brand) {
				t.Errorf("%s is titled %q: a screen titled with the name cannot tell its brand from its title", what, title)
			}
		case "bare":
			if got != "" {
				t.Errorf("%s %s drew the brand %q with no name on the request: it keeps a name of its own", kind, what, got)
			}
			if landing && title != "" {
				t.Errorf("the landing page %s is titled %q with no name on the request", what, title)
			}
		}
	}
	for run, lines := range seen {
		for _, w := range want {
			if !lines[w] {
				t.Errorf("%s: the probe never reached %s, so nothing here says what it draws; it printed:\n%s", run, w, out)
			}
		}
	}
}

// appNameProbe draws every screen the kit publishes, sends both messages and
// starts a second factor, printing the title and the name each one was handed.
const appNameProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/arandu-io/framework/http/middleware"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/security"
	twofactor "github.com/arandu-io/hesape/2fa"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"

	controllers "example.test/project/app/Http/Controllers"
	authui "example.test/project/app/Http/Controllers/Auth"
	appmail "example.test/project/app/Mail"
	models "example.test/project/app/Models"
)

var run string

var owner = &models.User{ID: "user-a", TenantID: "tenant-a", Name: "Ada Lovelace", Email: "a@example.test", Password: "stored"}

type users struct{ authui.Users }

func (users) PublicNames(context.Context, nativeauth.Subject, []string) (map[string]string, error) {
	return map[string]string{"user-a": "Ada Lovelace"}, nil
}
func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return owner, nil
}
func (users) FindForAuthentication(context.Context, string, string) (account.Account, error) {
	return owner, nil
}
func (users) Lookup(context.Context, string, string) (account.Account, error) { return owner, nil }
func (users) ConfirmPassword(context.Context, nativeauth.Subject, string, string) error { return nil }

type factors struct{ authui.Factors }

func (factors) Required(context.Context, string, string) (bool, error) { return true, nil }
func (factors) Begin(_ context.Context, _ nativeauth.Subject, issuer string) (twofactor.Provisioning, error) {
	fmt.Printf("%s|issuer|two-factor||%s\n", run, issuer)
	return twofactor.Provisioning{}, errors.New("the probe stops at the issuer")
}
func (factors) RegenerateRecoveryCodes(context.Context, nativeauth.Subject) ([]string, error) {
	return []string{"one", "two"}, nil
}

type codes struct{}

func (codes) Issue(context.Context, string, string) (string, error) { return "123456", nil }
func (codes) Consume(context.Context, string, string, string) error  { return nil }

type outbox struct{}

func (outbox) Name() string                             { return "probe" }
func (outbox) Send(context.Context, mail.Message) error { return nil }

type bodies struct{}

func (bodies) RenderToString(name string, data any) (string, error) {
	brand := "(not a message of the kit)"
	switch m := data.(type) {
	case appmail.VerifyEmail:
		brand = m.BrandName
	case appmail.PasswordReset:
		brand = m.BrandName
	}
	fmt.Printf("%s|mail|%s||%s\n", run, name, brand)
	return "body", nil
}

func main() {
	for _, name := range []string{
		"welcome", "home", "auth.login", "auth.register", "auth.passwords.email", "auth.passwords.reset",
		"auth.passwords.confirm", "auth.verify", "auth.two-factor.challenge", "auth.two-factor.recovery",
		"auth.two-factor.setup", "auth.two-factor.recovery-codes",
	} {
		view.Register(name, func(w io.Writer, data any) error {
			page := data.(authui.AuthPage)
			_, err := fmt.Fprintf(w, "%s|screen|%s|%s|%s", run, name, page.Title, page.AppName)
			return err
		})
	}

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	mailer := mail.New(outbox{}, bodies{}, mail.Address{Email: "noreply@example.test"})
	module := authui.New(users{}, factors{}, codes{}, sessions, session.NewCSRF(appKey, time.Hour), mailer,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	home := controllers.NewHomeController(users{}, "tenant-a")

	router := fhttp.NewRouter().WithRenderer(view.NewRenderer())
	module.Routes(router)
	router.Action("GET", "/{$}", home.Index, middleware.LoadSubject(sessions)).Name("home")

	for _, c := range []struct{ run, name string }{{"named", "Brandproof Ltd"}, {"bare", ""}} {
		run = c.run
		// What the framework does before every route: the configured name on
		// the request context, and nothing when none is configured.
		app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			router.ServeHTTP(w, r.WithContext(hhttp.WithAppName(r.Context(), c.name)))
		})

		jar := map[string]*http.Cookie{}
		send := func(method, path string, form url.Values) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			r.Header.Set("Accept", "text/html")
			if form != nil {
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			for _, cookie := range jar {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			for _, cookie := range w.Result().Cookies() {
				if cookie.MaxAge < 0 {
					delete(jar, cookie.Name)
				} else {
					jar[cookie.Name] = cookie
				}
			}
			return w
		}
		draw := func(method, path string, form url.Values) {
			w := send(method, path, form)
			if w.Code != http.StatusOK {
				fmt.Printf("%s|miss|%s %s|%d|\n", run, method, path, w.Code)
				return
			}
			fmt.Println(w.Body.String())
		}

		for _, path := range []string{"/", "/auth/login", "/auth/register", "/auth/password",
			"/auth/password/reset", "/auth/verify"} {
			draw("GET", path, nil)
		}
		send("POST", "/auth/verify/resend", url.Values{"email": {owner.Email}})
		send("POST", "/auth/password/email", url.Values{"email": {owner.Email}})

		// A password sign-in on an account with a second factor leaves the
		// pending cookie the two challenge screens ask for.
		send("POST", "/auth/login", url.Values{"email": {owner.Email}, "password": {"a-password-long-enough"}})
		draw("GET", "/auth/two-factor/challenge", nil)
		draw("GET", "/auth/two-factor/recovery", nil)

		signedIn := httptest.NewRecorder()
		if _, err := sessions.Rotate(context.Background(), signedIn, "",
			security.Subject{ID: "user-a", Tenant: "tenant-a"}, security.Remember(false)); err != nil {
			panic(err)
		}
		jar = map[string]*http.Cookie{}
		for _, cookie := range signedIn.Result().Cookies() {
			jar[cookie.Name] = cookie
		}
		draw("GET", "/", nil)
		draw("GET", "/auth/password/confirm", nil)
		send("POST", "/auth/password/confirm", url.Values{"password": {"a-password-long-enough"}})
		draw("GET", "/auth/two-factor/setup", nil)
		send("POST", "/auth/two-factor/setup", url.Values{})
		draw("POST", "/auth/two-factor/recovery-codes", url.Values{})
	}
}
`

// TestTheFlowRunsOnTheAccountPackage runs the published module the way a
// project wires it once its user and second-factor services are deleted:
// account.NewUsers over the project's users query and policy, account.NewFactors
// beside it, both handed to authui.New as they are, over a real SQLite database
// migrated with the users table, the outbox and account.Migrations.
//
// Every other probe here hands the module a fake service, which proves what the
// handlers do with an answer and nothing about whether the account package
// gives that answer. This one walks the whole flow against it: sign-up and the
// address it refuses twice, the verification code, sign-in with and without
// remember-me, the reset code and the password it changes, the password
// confirmation, two-factor setup with a real authenticator code, the challenge
// refusing a wrong code and taking a right one, and a recovery code spent once.
//
// Two more modules over the same tables carry the two options: WithRegistration
// with a sign-up form that asks a document number instead of a name, completed
// by account.Hooks.Prepare from the registration's Extra; and WithRemember(false),
// which draws no box and ignores a ticked one.
//
// It requires the SQLite connector the skeleton requires, by published tag like
// the rest, and skips like the compile gate when the tags are not to be had.
func TestTheFlowRunsOnTheAccountPackage(t *testing.T) {
	out := runAgainstPublishedKit(t, accountFlowProbe, "github.com/arandu-io/hesape/database/connectors/sqlite "+publishedSQLite)

	for _, want := range []string{
		"register: status=303 location=/auth/verify code=true",
		"register again: status=303 location=/auth/register",
		"register again screen: errors=email",
		"register short: errors=password,password_confirmation",
		"register no name: errors=name",
		"verify: status=303 location=/auth/login",
		"verify screen: status=Your address is confirmed. Welcome.",
		"verified: true",
		"login: status=303 location=/ remembered=false",
		"login remember: status=303 location=/ remembered=true",
		"login wrong: status=303 location=/auth/login",
		"login wrong screen: errors=email",
		"reset request: status=303 location=/auth/password/reset code=true",
		"reset: status=303 location=/auth/login",
		"login old password: status=303 location=/auth/login",
		"login new password: status=303 location=/",
		"confirm: status=303",
		"setup: secret=true",
		"setup confirm: recovery-codes=8",
		"login with factor: status=303 location=/auth/two-factor/challenge",
		"challenge wrong: status=303 location=/auth/two-factor/challenge",
		"challenge right: status=303 location=/ signed-in=true",
		"recovery: status=303 location=/ signed-in=true",
		"recovery spent: status=303 location=/auth/two-factor/recovery",
		"seam screen: errors=document",
		"seam register: status=303 location=/auth/verify",
		"seam name: Holder of 123.456.789-09",
		"withheld screen: offers-remember=false",
		"withheld login: status=303 location=/ remembered=false",
		"offered screen: offers-remember=true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit on the account package did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// publishedSQLite is the SQLite connector the flow probe migrates and runs on:
// the one the skeleton requires, so the probe's database is a new project's.
const publishedSQLite = "v0.11.0"

// accountFlowProbe is the program TestTheFlowRunsOnTheAccountPackage runs.
const accountFlowProbe = `package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database"
	_ "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/events"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/onetime"
	"github.com/arandu-io/hesape/otp"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
	"github.com/arandu-io/hesape/view"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

// userPolicy is the skeleton's user policy, written over *models.User.
type userPolicy struct{}

func (userPolicy) Can(_ context.Context, s auth.Subject, a auth.Action, u *models.User) error {
	if u.TenantID != "" && u.TenantID != s.Tenant {
		return fmt.Errorf("another tenant")
	}
	switch {
	case a == account.ActionUserCreate && s.IsGuest() && len(u.Roles) == 0:
		return nil
	case a == account.ActionUserNamesPublic && s.Tenant != "":
		return nil
	case (a == account.ActionUserView || a == account.ActionUserUpdate) && s.ID != "" && s.ID == u.ID:
		return nil
	}
	return fmt.Errorf("insufficient role for %s", a)
}

// createUsers is the skeleton's users table, both of its migrations at once.
type createUsers struct{ migrations.BaseMigration }

func (createUsers) GetName() string { return "create_users" }
func (createUsers) Up(ctx context.Context, conn migrations.Connection) error {
	return conn.Schema().Create(ctx, "users", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("email")
		table.Text("password")
		table.Text("roles")
		table.Timestamp("created_at")
		table.String("name").Nullable()
		table.Timestamp("verified_at").Nullable()
		table.Unique([]string{"tenant_id", "email"})
	})
}

// codes keeps the last code issued for each purpose, as the mailbox would.
type codes struct {
	onetime.CodeStore
	last map[string]string
}

func (c *codes) Issue(ctx context.Context, purpose, subject string) (string, error) {
	code, err := c.CodeStore.Issue(ctx, purpose, subject)
	c.last[purpose] = code
	return code, err
}

type outbox struct{}

func (outbox) Name() string                             { return "probe" }
func (outbox) Send(context.Context, mail.Message) error { return nil }

type bodies struct{}

func (bodies) RenderToString(string, any) (string, error) { return "body", nil }

// person is what the document form adds, carried in Registration.Extra.
type person struct{ Document string }

func readPerson(ctx *hhttp.Context, registration account.Registration) (account.Registration, error) {
	var in struct {
		Document string ` + "`form:\"document\"`" + `
	}
	if err := ctx.Bind(&in); err != nil {
		return registration, err
	}
	if in.Document == "" {
		return registration, validation.Errors{"document": {"type your document number"}}
	}
	registration.Extra = person{Document: in.Document}
	return registration, nil
}

var (
	secret        string
	recoveryCodes string
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	for _, name := range []string{
		"auth.login", "auth.register", "auth.verify", "auth.passwords.email", "auth.passwords.reset",
		"auth.passwords.confirm", "auth.two-factor.challenge", "auth.two-factor.recovery",
		"auth.two-factor.setup", "auth.two-factor.recovery-codes",
	} {
		view.Register(name, func(w io.Writer, data any) error {
			page := data.(authui.AuthPage)
			if page.SecretKey != "" {
				secret = page.SecretKey
			}
			if page.RecoveryCodesText != "" {
				recoveryCodes = page.RecoveryCodesText
			}
			var fields []string
			for field := range page.Page.Errors {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			_, err := fmt.Fprintf(w, "status=%s|errors=%s|offers-remember=%t", page.Status, strings.Join(fields, ","), page.OffersRemember())
			return err
		})
	}

	ctx := context.Background()
	raw, err := sql.Open("sqlite", ":memory:")
	must(err)
	raw.SetMaxOpenConns(1)
	db := database.Wrap(raw, database.DialectSQLite)
	conn := database.ForMigrations(database.NewConnection(raw, "", "", map[string]any{"driver": "sqlite", "name": "sqlite"}))
	all := append([]migrations.Migration{createUsers{}}, events.NewModule().Migrations()...)
	for _, m := range append(all, account.Migrations()...) {
		must(m.Up(ctx, conn))
	}

	appKey := []byte("0123456789abcdef0123456789abcdef")
	query := func() *model.Builder { return models.Users(db) }
	users, err := account.NewUsers(account.Config{DB: db, Query: query, Policy: account.PolicyOf(userPolicy{})})
	must(err)
	factors, err := account.NewFactors(db, appKey, cache.NewArrayStore(), nil, users)
	must(err)
	store, err := onetime.New(cache.NewArrayStore(), appKey, onetime.Config{})
	must(err)
	issued := &codes{CodeStore: store, last: map[string]string{}}
	mailer := mail.New(outbox{}, bodies{}, mail.Address{Email: "noreply@example.test"})
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	flash := session.NewFlash(appKey, false)

	// The person the document form registers gets a name from Prepare, which
	// reads what the form's reader left in Extra.
	seamUsers, err := account.NewUsers(account.Config{DB: db, Query: query, Policy: account.PolicyOf(userPolicy{}),
		Hooks: account.Hooks{Prepare: func(_ context.Context, registration *account.Registration, _ account.Account) error {
			if p, ok := registration.Extra.(person); ok && registration.Name == "" {
				registration.Name = "Holder of " + p.Document
			}
			return nil
		}}})
	must(err)

	mount := func(module *authui.Module) http.Handler {
		router := fhttp.NewRouter().WithFlash(flash)
		module.Routes(router)
		app := middleware.Flash(flash)(router)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.ServeHTTP(w, r.WithContext(hhttp.WithAppName(r.Context(), "Probe")))
		})
	}
	newModule := func(u authui.Users) *authui.Module {
		return authui.New(u, factors, issued, sessions, session.NewCSRF(appKey, time.Hour), mailer,
			nil, appKey, authui.FixedTenant("tenant-a"), false)
	}
	kit := mount(newModule(users))
	seam := mount(newModule(seamUsers).WithRegistration(readPerson))
	withheld := mount(newModule(users).WithRemember(false))

	type browser struct{ jar map[string]*http.Cookie }
	send := func(app http.Handler, b *browser, method, path string, form url.Values) *http.Response {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r := httptest.NewRequest(method, path, body)
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Referer", path)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, c := range b.jar {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		res := w.Result()
		for _, c := range res.Cookies() {
			if c.MaxAge < 0 {
				delete(b.jar, c.Name)
			} else {
				b.jar[c.Name] = c
			}
		}
		return res
	}
	screen := func(app http.Handler, b *browser, path string) string {
		res := send(app, b, "GET", path, nil)
		text, _ := io.ReadAll(res.Body)
		return string(text)
	}
	field := func(text, name string) string {
		for _, part := range strings.Split(text, "|") {
			if value, ok := strings.CutPrefix(part, name+"="); ok {
				return value
			}
		}
		return ""
	}
	remembered := func(b *browser) bool {
		for _, c := range b.jar {
			if c.MaxAge > int(time.Hour/time.Second) {
				return true
			}
		}
		return false
	}
	signedIn := func(b *browser) bool {
		_, err := sessions.Load(ctx, (&http.Request{Header: http.Header{"Cookie": cookieHeader(b.jar)}}))
		return err == nil
	}
	fresh := func() *browser { return &browser{jar: map[string]*http.Cookie{}} }
	at := func(res *http.Response) string { return fmt.Sprintf("status=%d location=%s", res.StatusCode, res.Header.Get("Location")) }

	const email, first, second = "ada@example.test", "a-first-password-long", "a-second-password-long"

	// Sign-up, the address taken, and a password the policy refuses.
	b := fresh()
	res := send(kit, b, "POST", "/auth/register", url.Values{"name": {"Ada"}, "email": {email},
		"password": {first}, "password_confirmation": {first}})
	fmt.Printf("register: %s code=%t\n", at(res), issued.last["verify-email"] != "")
	res = send(kit, b, "POST", "/auth/register", url.Values{"name": {"Ada"}, "email": {email},
		"password": {first}, "password_confirmation": {first}})
	fmt.Printf("register again: %s\n", at(res))
	fmt.Printf("register again screen: errors=%s\n", field(screen(kit, b, "/auth/register"), "errors"))
	send(kit, b, "POST", "/auth/register", url.Values{"name": {"Bo"}, "email": {"bo@example.test"},
		"password": {"short"}, "password_confirmation": {"other"}})
	fmt.Printf("register short: errors=%s\n", field(screen(kit, b, "/auth/register"), "errors"))
	send(kit, b, "POST", "/auth/register", url.Values{"email": {"cy@example.test"},
		"password": {first}, "password_confirmation": {first}})
	fmt.Printf("register no name: errors=%s\n", field(screen(kit, b, "/auth/register"), "errors"))

	// The verification code.
	res = send(kit, b, "POST", "/auth/verify/confirm", url.Values{"email": {email}, "email_code": {issued.last["verify-email"]}})
	fmt.Printf("verify: %s\n", at(res))
	fmt.Printf("verify screen: status=%s\n", field(screen(kit, b, "/auth/login"), "status"))
	verified, err := users.Lookup(ctx, "tenant-a", email)
	must(err)
	fmt.Printf("verified: %t\n", verified.IsVerified())

	// Sign-in, with and without remember-me, and a wrong password.
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {first}})
	fmt.Printf("login: %s remembered=%t\n", at(res), remembered(b))
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {first}, "remember": {"1"}})
	fmt.Printf("login remember: %s remembered=%t\n", at(res), remembered(b))
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {"not-the-password"}})
	fmt.Printf("login wrong: %s\n", at(res))
	fmt.Printf("login wrong screen: errors=%s\n", field(screen(kit, b, "/auth/login"), "errors"))

	// The reset code, and the password it changes.
	b = fresh()
	res = send(kit, b, "POST", "/auth/password/email", url.Values{"email": {email}})
	fmt.Printf("reset request: %s code=%t\n", at(res), issued.last["reset-password"] != "")
	res = send(kit, b, "POST", "/auth/password/update", url.Values{"email": {email},
		"email_code": {issued.last["reset-password"]}, "password": {second}, "password_confirmation": {second}})
	fmt.Printf("reset: %s\n", at(res))
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {first}})
	fmt.Printf("login old password: %s\n", at(res))
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {second}})
	fmt.Printf("login new password: %s\n", at(res))

	// The password confirmed, and the second factor set up with a real code.
	res = send(kit, b, "POST", "/auth/password/confirm", url.Values{"password": {second}})
	fmt.Printf("confirm: status=%d\n", res.StatusCode)
	send(kit, b, "POST", "/auth/two-factor/setup", url.Values{})
	fmt.Printf("setup: secret=%t\n", secret != "")
	key, err := otp.DecodeSecret(secret)
	must(err)
	code := func(at time.Time) string {
		c, err := otp.Default().Generate(key, at)
		must(err)
		return c
	}
	send(kit, b, "POST", "/auth/two-factor/setup/confirm", url.Values{"authenticator_code": {code(time.Now().Add(-30 * time.Second))}})
	fmt.Printf("setup confirm: recovery-codes=%d\n", len(strings.Fields(recoveryCodes)))

	// The challenge: a wrong code goes back, a right one signs in.
	b = fresh()
	res = send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {second}})
	fmt.Printf("login with factor: %s\n", at(res))
	res = send(kit, b, "POST", "/auth/two-factor/challenge", url.Values{"authenticator_code": {"000000"}})
	fmt.Printf("challenge wrong: %s\n", at(res))
	res = send(kit, b, "POST", "/auth/two-factor/challenge", url.Values{"authenticator_code": {code(time.Now().Add(30 * time.Second))}})
	fmt.Printf("challenge right: %s signed-in=%t\n", at(res), signedIn(b))

	// A recovery code signs in once.
	spend := strings.Fields(recoveryCodes)[0]
	b = fresh()
	send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {second}})
	res = send(kit, b, "POST", "/auth/two-factor/recovery", url.Values{"recovery_code": {spend}})
	fmt.Printf("recovery: %s signed-in=%t\n", at(res), signedIn(b))
	b = fresh()
	send(kit, b, "POST", "/auth/login", url.Values{"email": {email}, "password": {second}})
	res = send(kit, b, "POST", "/auth/two-factor/recovery", url.Values{"recovery_code": {spend}})
	fmt.Printf("recovery spent: %s\n", at(res))

	// The registration seam: a form with a document number and no name.
	b = fresh()
	send(seam, b, "POST", "/auth/register", url.Values{"email": {"doc@example.test"},
		"password": {first}, "password_confirmation": {first}})
	fmt.Printf("seam screen: errors=%s\n", field(screen(seam, b, "/auth/register"), "errors"))
	res = send(seam, b, "POST", "/auth/register", url.Values{"email": {"doc@example.test"}, "document": {"123.456.789-09"},
		"password": {first}, "password_confirmation": {first}})
	fmt.Printf("seam register: %s\n", at(res))
	holder, err := users.Lookup(ctx, "tenant-a", "doc@example.test")
	must(err)
	fmt.Printf("seam name: %s\n", holder.AccountName())

	// Remember-me withheld: no box, and a ticked one ignored.
	b = fresh()
	fmt.Printf("withheld screen: offers-remember=%s\n", field(screen(withheld, b, "/auth/login"), "offers-remember"))
	res = send(withheld, b, "POST", "/auth/login", url.Values{"email": {"doc@example.test"}, "password": {first}, "remember": {"1"}})
	fmt.Printf("withheld login: %s remembered=%t\n", at(res), remembered(b))
	fmt.Printf("offered screen: offers-remember=%s\n", field(screen(kit, fresh(), "/auth/login"), "offers-remember"))
}

func cookieHeader(jar map[string]*http.Cookie) []string {
	var parts []string
	for _, c := range jar {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return []string{strings.Join(parts, "; ")}
}
`

// TestACodeIsGuessedAgainstOneBudgetPerAddressAndOnePerClient runs the two
// forms that check a code the kit mailed -- the new password and the address
// confirmation -- with a wrong code, over and over.
//
// A code allows a few tries and every new one starts its own, so whoever can
// ask for codes could guess for as long as they kept asking: thousands of
// guesses a day against one account. The address typed is what bounds that,
// whichever client the guesses come from and however the address is spelled,
// and it is counted before anything is looked up, so an address with no
// account runs out exactly when one with an account does. The client is what
// bounds walking a list of addresses from one place. A refused guess never
// reaches the code store.
func TestACodeIsGuessedAgainstOneBudgetPerAddressAndOnePerClient(t *testing.T) {
	out := runAgainstPublishedKit(t, codeGuessProbe)

	for _, want := range []string{
		"password.update address registered: allowed=10 consumed=10 refused=303 retry-after=true",
		"password.update address unregistered: allowed=10 consumed=0 refused=303 retry-after=true",
		"password.update client: allowed=30 refused=303 retry-after=true",
		"verify.confirm address registered: allowed=10 consumed=10 refused=303 retry-after=true",
		"verify.confirm address unregistered: allowed=10 consumed=0 refused=303 retry-after=true",
		"verify.confirm client: allowed=30 refused=303 retry-after=true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// codeGuessProbe offers each code form a wrong code until it is refused: first
// one address from a new client each time, spelled two ways, then one client
// with a new address each time.
const codeGuessProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/session"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

var owner = &models.User{ID: "user-a", TenantID: "tenant-a", Name: "Ada", Email: "a@example.test", Password: "stored"}

type users struct{ authui.Users }

func (users) Lookup(_ context.Context, _, email string) (account.Account, error) {
	if account.NormalizeEmail(email) == owner.Email {
		return owner, nil
	}
	return nil, errors.New("no such account")
}

type codes struct{ consumed int }

func (*codes) Issue(context.Context, string, string) (string, error) { return "123456", nil }
func (c *codes) Consume(context.Context, string, string, string) error {
	c.consumed++
	return errors.New("that is not the code")
}

func main() {
	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	store := &codes{}
	module := authui.New(users{}, nil, store, sessions, session.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	post := func(path, client string, form url.Values) *http.Response {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", path)
		r.RemoteAddr = client + ":40000"
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w.Result()
	}
	// until posts until the form is refused with a Retry-After, and returns how
	// many it let through first and the refusal; a form that refuses nothing in
	// fifty is answered as fifty allowed.
	until := func(next func(i int) *http.Response) (int, *http.Response) {
		var res *http.Response
		for i := 0; i < 50; i++ {
			if res = next(i); res.Header.Get("Retry-After") != "" {
				return i, res
			}
		}
		return 50, res
	}

	const password = "a-password-long-enough"
	for _, f := range []struct {
		name, path string
		form       func(email string) url.Values
	}{
		{"password.update", "/auth/password/update", func(email string) url.Values {
			return url.Values{"email": {email}, "email_code": {"000000"}, "password": {password},
				"password_confirmation": {password}}
		}},
		{"verify.confirm", "/auth/verify/confirm", func(email string) url.Values {
			return url.Values{"email": {email}, "email_code": {"000000"}}
		}},
	} {
		for _, a := range []struct{ kind, email string }{
			{"registered", owner.Email}, {"unregistered", "nobody@example.test"},
		} {
			store.consumed = 0
			allowed, res := until(func(i int) *http.Response {
				spelled := a.email
				if i%2 == 1 {
					spelled = "  " + strings.ToUpper(a.email)
				}
				return post(f.path, fmt.Sprintf("198.51.100.%d", i+1), f.form(spelled))
			})
			fmt.Printf("%s address %s: allowed=%d consumed=%d refused=%d retry-after=%t\n", f.name, a.kind,
				allowed, store.consumed, res.StatusCode, res.Header.Get("Retry-After") != "")
		}
		allowed, res := until(func(i int) *http.Response {
			return post(f.path, "203.0.113.7", f.form(fmt.Sprintf("guess-%d@example.test", i)))
		})
		fmt.Printf("%s client: allowed=%d refused=%d retry-after=%t\n", f.name, allowed, res.StatusCode,
			res.Header.Get("Retry-After") != "")
	}
}
`

// TestAskingForACodeIsLimitedPerAddressAndPerClient runs the two forms that
// mail a code -- the reset request and the verification resend -- through a
// counter that records the keys it was asked to count, one that says every
// budget is spent, and one that cannot count at all.
//
// They are limited like the forms that check a code: once per client and once
// per address, the address counted the same however it is spelled. A spent
// budget and a store that cannot count both refuse the form before anything is
// looked up or mailed: the second is what keeps a cache outage from taking the
// limit away. Ten real requests are not spent reaching a limit here: the
// guessing probe already counts the same limits for real.
func TestAskingForACodeIsLimitedPerAddressAndPerClient(t *testing.T) {
	out := runAgainstPublishedKit(t, askForCodeLimitProbe)

	for _, want := range []string{
		"password.email keys: first=2 same-address-new-client=3",
		"verify.resend keys: first=2 same-address-new-client=3",
		"password.email spent: status=303 retry-after=true looked-up=false mailed=false",
		"verify.resend spent: status=303 retry-after=true looked-up=false mailed=false",
		"password.email uncounted: status=303 looked-up=false mailed=false",
		"verify.resend uncounted: status=303 looked-up=false mailed=false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// askForCodeLimitProbe counts the keys one address is limited by, then asks
// once more with every budget spent and once with a store that cannot count.
const askForCodeLimitProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/session"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

var owner = &models.User{ID: "user-a", TenantID: "tenant-a", Name: "Ada", Email: "a@example.test", Password: "stored"}

var (
	mu       sync.Mutex
	lookedUp int
	sent     int
)

type users struct{ authui.Users }

func (users) Lookup(_ context.Context, _, email string) (account.Account, error) {
	mu.Lock()
	lookedUp++
	mu.Unlock()
	if account.NormalizeEmail(email) == owner.Email {
		return owner, nil
	}
	return nil, errors.New("no such account")
}

type codes struct{}

func (codes) Issue(context.Context, string, string) (string, error) { return "123456", nil }
func (codes) Consume(context.Context, string, string, string) error  { return errors.New("unused") }

type outbox struct{}

func (outbox) Name() string { return "probe" }
func (outbox) Send(context.Context, mail.Message) error {
	mu.Lock()
	sent++
	mu.Unlock()
	return nil
}

type bodies struct{}

func (bodies) RenderToString(string, any) (string, error) { return "body", nil }

// counter is the cache store the limiter counts in, with the keys it was asked
// to count, a switch that answers every count as spent, and one that answers
// every count with an error, as a store that went away does.
type counter struct {
	*cache.ArrayStore
	full   bool
	broken bool
	keys   map[string]bool
}

func (c *counter) Increment(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	c.keys[key] = true
	if c.broken {
		return 0, errors.New("the cache went away")
	}
	if c.full {
		return 1 << 30, nil
	}
	return c.ArrayStore.Increment(ctx, key, delta, ttl)
}

func read(n *int) int {
	mu.Lock()
	defer mu.Unlock()
	return *n
}

func main() {
	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	mailer := mail.New(outbox{}, bodies{}, mail.Address{Email: "noreply@example.test"})
	limits := &counter{ArrayStore: cache.NewArrayStore(), keys: map[string]bool{}}
	module := authui.New(users{}, nil, codes{}, sessions, session.NewCSRF(appKey, time.Hour), mailer,
		cache.NewRateLimiter(limits), appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	post := func(path, client, email string) *http.Response {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"email": {email}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", path)
		r.RemoteAddr = client + ":40000"
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w.Result()
	}

	for _, f := range []struct{ name, path string }{
		{"password.email", "/auth/password/email"}, {"verify.resend", "/auth/verify/resend"},
	} {
		limits.keys = map[string]bool{}
		post(f.path, "203.0.113.1", "  Someone@Example.test")
		first := len(limits.keys)
		post(f.path, "203.0.113.2", "someone@example.test")
		fmt.Printf("%s keys: first=%d same-address-new-client=%d\n", f.name, first, len(limits.keys))

		limits.full = true
		looked, mailed := read(&lookedUp), read(&sent)
		res := post(f.path, "203.0.113.3", owner.Email)
		limits.full = false
		fmt.Printf("%s spent: status=%d retry-after=%t looked-up=%t mailed=%t\n", f.name, res.StatusCode,
			res.Header.Get("Retry-After") != "", read(&lookedUp) != looked, read(&sent) != mailed)

		limits.broken = true
		looked, mailed = read(&lookedUp), read(&sent)
		res = post(f.path, "203.0.113.4", owner.Email)
		limits.broken = false
		fmt.Printf("%s uncounted: status=%d looked-up=%t mailed=%t\n", f.name, res.StatusCode,
			read(&lookedUp) != looked, read(&sent) != mailed)
	}
}
`

// TestAskingForACodeSaysNothingAboutTheAddressInItsTime runs the two forms that
// mail a code for an address with an account and one without, and with a
// mailer that is quick and one that is slow.
//
// Both answer with the same sentence, and the sentence was never the leak: the
// address with an account is issued a code and mailed before the answer, the
// one without is not, and the difference is the time. Each answer has to take
// as long as the others, the quick message has to be out before its answer and
// the slow one has to be sent after it, rather than lost.
func TestAskingForACodeSaysNothingAboutTheAddressInItsTime(t *testing.T) {
	out := runAgainstPublishedKit(t, askForCodeTimeProbe)

	for _, form := range []string{"password.email", "verify.resend"} {
		line := regexp.MustCompile(form + ` time: registered=(\d+) unregistered=(\d+) slow=(\d+) ` +
			`sent-before-answer=(\w+) slow-sent-after=(\w+)`).FindStringSubmatch(out)
		if line == nil {
			t.Errorf("the probe printed no timing for %s; it printed:\n%s", form, out)
			continue
		}
		registered, _ := strconv.Atoi(line[1])
		unregistered, _ := strconv.Atoi(line[2])
		slow, _ := strconv.Atoi(line[3])
		for _, c := range []struct {
			name string
			ms   int
		}{{"an address with an account", registered}, {"an address with an account and a slow mailer", slow}} {
			if d := c.ms - unregistered; d > 250 || d < -250 {
				t.Errorf("%s answers %s in %dms and an address without one in %dms: the time says which "+
					"addresses have accounts", form, c.name, c.ms, unregistered)
			}
		}
		if line[4] != "true" {
			t.Errorf("%s answered before a quick mailer had sent the code", form)
		}
		if line[5] != "true" {
			t.Errorf("%s never sent the code the slow mailer was still sending when it answered", form)
		}
	}
}

// askForCodeTimeProbe times each form for an address with an account, one
// without, and one with an account behind a mailer slower than the answer.
const askForCodeTimeProbe = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/mail"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/session"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

var owner = &models.User{ID: "user-a", TenantID: "tenant-a", Name: "Ada", Email: "a@example.test", Password: "stored"}

var (
	mu    sync.Mutex
	sent  int
	delay time.Duration
)

type users struct{ authui.Users }

func (users) Lookup(_ context.Context, _, email string) (account.Account, error) {
	if account.NormalizeEmail(email) == owner.Email {
		return owner, nil
	}
	return nil, errors.New("no such account")
}

type codes struct{}

func (codes) Issue(context.Context, string, string) (string, error) { return "123456", nil }
func (codes) Consume(context.Context, string, string, string) error  { return errors.New("unused") }

type outbox struct{}

func (outbox) Name() string { return "probe" }
func (outbox) Send(context.Context, mail.Message) error {
	mu.Lock()
	wait := delay
	mu.Unlock()
	time.Sleep(wait)
	mu.Lock()
	sent++
	mu.Unlock()
	return nil
}

type bodies struct{}

func (bodies) RenderToString(string, any) (string, error) { return "body", nil }

func sentSoFar() int {
	mu.Lock()
	defer mu.Unlock()
	return sent
}

func slowMailer(d time.Duration) {
	mu.Lock()
	delay = d
	mu.Unlock()
}

func main() {
	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	mailer := mail.New(outbox{}, bodies{}, mail.Address{Email: "noreply@example.test"})
	module := authui.New(users{}, nil, codes{}, sessions, session.NewCSRF(appKey, time.Hour), mailer,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	post := func(path, email string) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"email": {email}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", path)
		w := httptest.NewRecorder()
		start := time.Now()
		app.ServeHTTP(w, r)
		return int(time.Since(start) / time.Millisecond)
	}

	for _, f := range []struct{ name, path string }{
		{"password.email", "/auth/password/email"}, {"verify.resend", "/auth/verify/resend"},
	} {
		before := sentSoFar()
		registered := post(f.path, owner.Email)
		sentBeforeAnswer := sentSoFar() == before+1
		unregistered := post(f.path, "nobody@example.test")

		slowMailer(1500 * time.Millisecond)
		before = sentSoFar()
		slow := post(f.path, owner.Email)
		deadline := time.Now().Add(4 * time.Second)
		for sentSoFar() == before && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		slowSentAfter := sentSoFar() == before+1
		slowMailer(0)

		fmt.Printf("%s time: registered=%d unregistered=%d slow=%d sent-before-answer=%t slow-sent-after=%t\n",
			f.name, registered, unregistered, slow, sentBeforeAnswer, slowSentAfter)
	}
}
`

// TestTheScreensThatShowASecondFactorSecretAreNeverCached runs the three
// answers that carry second-factor key material -- the setup screen with the
// authenticator secret and its QR code, and the recovery codes shown after a
// confirmation and after a regeneration -- and reads their Cache-Control.
//
// Each is a credential on a page. A page a browser or a proxy keeps is one the
// back button, a shared computer or a cache can show again to somebody else.
func TestTheScreensThatShowASecondFactorSecretAreNeverCached(t *testing.T) {
	out := runAgainstPublishedKit(t, noStoreProbe)

	for _, want := range []string{
		"setup: status=200 no-store=true",
		"confirm: status=200 no-store=true",
		"regenerate: status=200 no-store=true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// noStoreProbe signs a session in, confirms its password and asks for each of
// the three answers that show a second-factor secret.
const noStoreProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	twofactor "github.com/arandu-io/hesape/2fa"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/view"

	authui "example.test/project/app/Http/Controllers/Auth"
)

type users struct{ authui.Users }

func (users) ConfirmPassword(context.Context, nativeauth.Subject, string, string) error { return nil }
func (users) PublicNames(context.Context, nativeauth.Subject, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

type factors struct{ authui.Factors }

func (factors) Begin(context.Context, nativeauth.Subject, string) (twofactor.Provisioning, error) {
	return twofactor.Provisioning{Issuer: "Probe", Account: "a@example.test",
		Secret: []byte("0123456789abcdefghij")}, nil
}
func (factors) Confirm(context.Context, nativeauth.Subject, string) ([]string, error) {
	return []string{"one", "two"}, nil
}
func (factors) RegenerateRecoveryCodes(context.Context, nativeauth.Subject) ([]string, error) {
	return []string{"three", "four"}, nil
}

func main() {
	for _, name := range []string{"auth.two-factor.setup", "auth.two-factor.recovery-codes"} {
		view.Register(name, func(w io.Writer, data any) error {
			_, err := io.WriteString(w, data.(authui.AuthPage).Title)
			return err
		})
	}

	appKey := []byte("0123456789abcdef0123456789abcdef")
	sessions := security.NewSessionStore(appKey, time.Hour, false,
		security.NewSessionBackend(session.NewArrayHandler[security.Subject]()))
	module := authui.New(users{}, factors{}, nil, sessions, security.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter().WithRenderer(view.NewRenderer())
	module.Routes(router)

	signedIn := httptest.NewRecorder()
	if _, err := sessions.Rotate(context.Background(), signedIn, "",
		security.Subject{ID: "user-a", Tenant: "tenant-a"}, security.Remember(false)); err != nil {
		panic(err)
	}
	cookies := signedIn.Result().Cookies()
	post := func(path string, form url.Values) *http.Response {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "text/html")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		res := w.Result()
		for _, c := range res.Cookies() {
			for i := range cookies {
				if cookies[i].Name == c.Name {
					cookies[i] = c
				}
			}
		}
		return res
	}
	noStore := func(res *http.Response) bool {
		return strings.Contains(res.Header.Get("Cache-Control"), "no-store")
	}

	post("/auth/password/confirm", url.Values{"password": {"a-password-long-enough"}})
	res := post("/auth/two-factor/setup", url.Values{})
	fmt.Printf("setup: status=%d no-store=%t\n", res.StatusCode, noStore(res))
	res = post("/auth/two-factor/setup/confirm", url.Values{"authenticator_code": {"123456"}})
	fmt.Printf("confirm: status=%d no-store=%t\n", res.StatusCode, noStore(res))
	res = post("/auth/two-factor/recovery-codes", url.Values{})
	fmt.Printf("regenerate: status=%d no-store=%t\n", res.StatusCode, noStore(res))
}
`

// TestTheSignedInSubjectIsTheOneTheAccountBuilds signs in two accounts through
// the published form and reads the subject the session was written with.
//
// The session subject is built in one place, the application's account: the
// skeleton's User.Subject, the method its session resolver reads again on every
// request. The kit kept a builder of its own beside it, which copied the id,
// the tenant, the verification and the roles field by field, so the subject a
// sign-in wrote and the one every later request read were two functions that
// could disagree. The first account here answers a subject that differs from
// what its readers say -- verified, while IsVerified says not -- so the session
// shows which of the two the sign-in asked. The second answers no subject at
// all, and is refused rather than signed in with whatever the kit could guess.
func TestTheSignedInSubjectIsTheOneTheAccountBuilds(t *testing.T) {
	out := runAgainstPublishedKit(t, accountSubjectProbe)

	for _, want := range []string{
		"builds: status=303 written=1 id=user-a tenant=tenant-a verified=true",
		"builds none: status=500 written=0",
		"the account main.bare does not build its session subject",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the published kit did not answer %q; it printed:\n%s", want, out)
		}
	}
}

// accountSubjectProbe signs in an account that builds its own subject and one
// that does not, over a session backend that records what it is given.
const accountSubjectProbe = `package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	nativeauth "github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/session"

	authui "example.test/project/app/Http/Controllers/Auth"
	models "example.test/project/app/Models"
)

var owner = &models.User{ID: "user-a", TenantID: "tenant-a", Name: "Ada", Email: "a@example.test", Password: "stored"}

// builds answers a subject that its readers would not have produced: verified,
// while IsVerified, read off the stored row, says the address is not.
type builds struct{ *models.User }

func (b builds) Subject() nativeauth.Subject {
	return nativeauth.Subject{ID: b.ID, Tenant: b.TenantID, Verified: true}
}

// bare is an account and nothing more: it answers no subject.
type bare struct{ account.Account }

var signingIn account.Account

type users struct{ authui.Users }

func (users) VerifyCredentials(context.Context, string, string, string, string) (account.Account, error) {
	return signingIn, nil
}

type factors struct{ authui.Factors }

func (factors) Required(context.Context, string, string) (bool, error) { return false, nil }

// recording keeps every subject a session is written with.
type recording struct {
	security.SessionBackend
	written []security.Subject
}

func (r *recording) Put(ctx context.Context, id string, s security.Subject, ttl time.Duration) error {
	r.written = append(r.written, s)
	return r.SessionBackend.Put(ctx, id, s, ttl)
}

func main() {
	appKey := []byte("0123456789abcdef0123456789abcdef")
	backend := &recording{SessionBackend: security.NewMemoryBackend()}
	sessions := security.NewSessionStore(appKey, time.Hour, false, backend)
	module := authui.New(users{}, factors{}, nil, sessions, session.NewCSRF(appKey, time.Hour), nil,
		nil, appKey, authui.FixedTenant("tenant-a"), false)
	router := fhttp.NewRouter()
	module.Routes(router)
	app := middleware.Flash(session.NewFlash(appKey, false))(router)

	signIn := func() int {
		form := url.Values{"email": {owner.Email}, "password": {"a-password-long-enough"}}
		r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", "/auth/login")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w.Code
	}

	signingIn = builds{owner}
	status := signIn()
	fmt.Printf("builds: status=%d written=%d", status, len(backend.written))
	for _, s := range backend.written {
		fmt.Printf(" id=%s tenant=%s verified=%t", s.ID, s.Tenant, s.Verified)
	}
	fmt.Println()

	backend.written = nil
	signingIn = bare{owner}
	status = signIn()
	fmt.Printf("builds none: status=%d written=%d\n", status, len(backend.written))
}
`

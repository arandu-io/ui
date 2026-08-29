package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAuthPublicationBoundary(t *testing.T) {
	files := mustGenerateAuth(t)
	var goFiles, views int
	for _, file := range files {
		path := filepath.ToSlash(file.Path)
		switch {
		case strings.HasSuffix(path, ".kyse.go"):
			views++
		case strings.HasSuffix(path, ".go"):
			goFiles++
		}
		for _, forbidden := range []string{
			"framework/modules/auth",
			"auth.Module",
			"auth.New(",
			"auth.Service",
			"auth.TenantResolver",
			"kernel.Migratable",
		} {
			if strings.Contains(string(file.Content), forbidden) {
				t.Errorf("%s publishes forbidden legacy boundary %q", path, forbidden)
			}
		}
		if strings.HasSuffix(path, ".py") {
			t.Errorf("the Go kit published Python tooling: %s", path)
		}
	}
	if goFiles != 10 || views != 18 || len(files) != 28 {
		t.Fatalf("published Go/views/total = %d/%d/%d, want 10/18/28", goFiles, views, len(files))
	}

	module := authFile(t, "Auth/LoginController.go")
	if strings.Count(module, "kernel.Module = (*Module)(nil)") != 1 {
		t.Error("generated Module does not prove its single route-only kernel.Module contract")
	}
	for _, nativeSigner := range []string{
		`"github.com/arandu-io/hesape/encryption"`,
		"signer   *encryption.Signer",
		"signer: encryption.NewSigner(appKey)",
	} {
		if !strings.Contains(module, nativeSigner) {
			t.Errorf("pending sign-in does not use the native signer contract %q", nativeSigner)
		}
	}
	if strings.Contains(module, "security.NewSigner") {
		t.Error("pending sign-in reaches the native signer through the framework alias")
	}
}

func TestNativeAuthRouteContractIsExact(t *testing.T) {
	module := authFile(t, "Auth/LoginController.go")
	if routes, custom := strings.Index(module, `g.Get("/two-factor/challenge"`), strings.Index(module, "// arandu:begin custom"); routes < 0 || custom < 0 || routes > custom {
		t.Fatal("required two-factor routes sit inside the replaceable custom block")
	}
	routes := publishedRoutes(t)
	if len(routes) != 23 {
		t.Fatalf("published %d auth routes, want 23", len(routes))
	}
	want := []registration{
		{"Get", "/two-factor/challenge", "auth.two-factor.challenge"},
		{"Post", "/two-factor/challenge", ""},
		{"Get", "/two-factor/recovery", "auth.two-factor.recovery"},
		{"Post", "/two-factor/recovery", ""},
		{"Get", "/two-factor/setup", "auth.two-factor.setup"},
		{"Post", "/two-factor/setup", ""},
		{"Post", "/two-factor/setup/confirm", "auth.two-factor.setup.confirm"},
		{"Post", "/two-factor/disable", "auth.two-factor.disable"},
		{"Post", "/two-factor/recovery-codes", "auth.two-factor.recovery-codes"},
	}
	var got []registration
	for _, route := range routes {
		if strings.HasPrefix(route.path, "/two-factor/") {
			got = append(got, route)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("published %d two-factor routes, want exactly 9: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("two-factor route %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNativeCodeInputsHaveExactAutocompleteContracts(t *testing.T) {
	checks := []struct {
		file, name, autocomplete string
	}{
		{"verify.kyse.go", "email_code", "one-time-code"},
		{"reset.kyse.go", "email_code", "one-time-code"},
		{"challenge.kyse.go", "authenticator_code", "one-time-code"},
		{"setup.kyse.go", "authenticator_code", "one-time-code"},
		{"recovery.kyse.go", "recovery_code", "off"},
	}
	for _, check := range checks {
		source := authFile(t, check.file)
		if !strings.Contains(source, `Name: "`+check.name+`"`) ||
			!strings.Contains(source, `Autocomplete: "`+check.autocomplete+`"`) {
			t.Errorf("%s does not publish %s with autocomplete=%s", check.file, check.name, check.autocomplete)
		}
	}
	for _, file := range mustGenerateAuth(t) {
		if strings.Contains(string(file.Content), "OTPInput") {
			t.Errorf("%s publishes the forbidden generic OTPInput contract", file.Path)
		}
	}
}

func TestEmailMutationsUsePurposeBoundNativeCodes(t *testing.T) {
	routes := bodyOf(t, authFile(t, "Auth/LoginController.go"), "Routes")
	if strings.Contains(routes, `g.Get("/verify/confirm"`) ||
		!strings.Contains(routes, `g.Post("/verify/confirm"`) {
		t.Error("email verification must mutate only through POST")
	}
	registration := authFile(t, "RegisterController.go")
	password := authFile(t, "PasswordController.go")
	for source, purpose := range map[string]string{
		registration: `"verify-email"`,
		password:     `"reset-password"`,
	} {
		if !strings.Contains(source, purpose) ||
			!strings.Contains(source, "m.codes.Issue(") ||
			!strings.Contains(source, "m.codes.Consume(") {
			t.Errorf("purpose-bound native code flow is incomplete for %s", purpose)
		}
	}
	for _, legacy := range []string{"ResetToken", `name="token"`, `?token=`, "m.signer.Sign("} {
		if strings.Contains(registration+password, legacy) {
			t.Errorf("email flow still publishes legacy signed-link element %q", legacy)
		}
	}
	if subject := bodyOf(t, registration, "emailCodeSubject"); !strings.Contains(subject, "u.TenantID") || !strings.Contains(subject, "u.ID") ||
		!strings.Contains(subject, "services.NormalizeEmail(u.Email)") {
		t.Errorf("verification codes are not bound to tenant, user and canonical e-mail: %s", subject)
	}
}

func TestVerificationSubjectChangesWithTheCanonicalAddress(t *testing.T) {
	registration := authFile(t, "RegisterController.go")
	subject := bodyOf(t, registration, "emailCodeSubject")
	for _, want := range []string{"u.TenantID", "u.ID", "services.NormalizeEmail(u.Email)"} {
		if !strings.Contains(subject, want) {
			t.Fatalf("emailCodeSubject omits %s: %s", want, subject)
		}
	}
	if strings.Count(registration, "emailCodeSubject(u)") != 2 {
		t.Fatal("Issue and Consume do not share the address-bound verification subject helper")
	}
}

func TestRegistrationRejectsAnAddressTheApplicationAlreadyOwns(t *testing.T) {
	body := bodyOf(t, authFile(t, "RegisterController.go"), "doRegister")
	if !strings.Contains(body, "errors.Is(err, services.ErrEmailTaken)") ||
		!strings.Contains(body, `"email": {"that address is already registered. Sign in instead."}`) {
		t.Error("application-owned ErrEmailTaken is not returned as a field-level 422 rejection")
	}
}

func TestPasswordCodeResponseDrawsTheFormThatConsumesIt(t *testing.T) {
	body := bodyOf(t, authFile(t, "PasswordController.go"), "sendPasswordCode")
	if !strings.Contains(body, `m.screen(w, r, "auth.passwords.reset"`) {
		t.Error("a sent reset code leaves the user on a screen with nowhere to type it")
	}
	if !strings.Contains(authFile(t, "reset.kyse.go"), ".Status") {
		t.Error("the reset form drops the anti-enumerated send outcome")
	}
}

func TestSessionCreationWaitsForTheFinalFactor(t *testing.T) {
	files := mustGenerateAuth(t)
	var all strings.Builder
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".go") && !strings.HasSuffix(file.Path, ".kyse.go") {
			all.Write(file.Content)
		}
	}
	if strings.Count(all.String(), ".sessions.Rotate(") != 1 {
		t.Fatalf("published session rotation count = %d, want one final seam", strings.Count(all.String(), ".sessions.Rotate("))
	}
	handlers := authFile(t, "LoginController_handlers.go")
	if strings.Contains(bodyOf(t, handlers, "doLogin"), ".sessions.Rotate(") ||
		!strings.Contains(bodyOf(t, handlers, "finishSignIn"), ".sessions.Rotate(") {
		t.Error("password login rotates outside the final sign-in seam")
	}

	twoFactor := authFile(t, "TwoFactorController.go")
	for _, check := range []struct{ function, verified string }{
		{"verifyTwoFactorChallenge", "VerifyAuthenticator("},
		{"verifyRecoveryChallenge", "ConsumeRecovery("},
	} {
		body := bodyOf(t, twoFactor, check.function)
		verified := strings.Index(body, check.verified)
		finished := strings.Index(body, "m.finishSignIn(")
		if verified < 0 || finished < 0 || finished < verified {
			t.Errorf("%s can finish before %s succeeds", check.function, check.verified)
		}
	}
	for _, function := range []string{"verifyTwoFactorChallenge", "verifyRecoveryChallenge", "confirmTwoFactorSetup"} {
		body := bodyOf(t, twoFactor, function)
		if !strings.Contains(body, "errors.Is(err, twofactor.ErrInvalidCode)") ||
			!strings.Contains(body, "http.StatusInternalServerError") {
			t.Errorf("%s does not separate an invalid factor from a storage failure", function)
		}
	}

	for _, required := range []string{
		`"two-factor-pending"`,
		`5 * time.Minute`,
		`Path: "/auth/two-factor"`,
		"HttpOnly: true",
		"SameSite: http.SameSiteLaxMode",
		"PasswordFingerprint: u.PasswordFingerprint()",
	} {
		if !strings.Contains(twoFactor, required) {
			t.Errorf("pending sign-in is missing %q", required)
		}
	}
	if strings.Contains(twoFactor, "u.Password,") {
		t.Error("pending sign-in carries the password hash instead of its full fingerprint")
	}
	if !strings.Contains(bodyOf(t, twoFactor, "beginTwoFactorSetup"),
		"m.factors.Begin(r.Context(), subject, m.appName)") {
		t.Error("two-factor provisioning does not use the application name as issuer")
	}
}

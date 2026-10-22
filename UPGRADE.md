# Upgrade guide

What a project changes when it publishes a release of this kit over files an
earlier one wrote: what a republish replaces on its own, what needs `--force`,
and what is edited by hand in files the kit never touches, such as
`bootstrap/app.go`.

Additions a project can ignore are not listed. The published files are the
project's from the moment they land, so an entry here is about the files a
project receives, not about this repository's code.

---

## v0.27.0 — the session subject is the account's, an unreadable session is logged, and spent codes say so

Three changes in the published controllers, written for hesape v0.77.0, which
reads a session's roles again on every request through a resolver the
application installs:

- The sign-in builds the session subject with the account's own `Subject()` --
  `User.Subject` in `app/Models/User.go`, the method the session resolver calls
  -- instead of `subjectOf`, the kit's copy of it. `LoginController_handlers.go`
  asserts that `*models.User` has the method, so a model without it stops the
  build; an account that answers no subject at run time is refused with a
  logged 500 rather than signed in with a guess. The reset signs the account's
  other sessions out through the same method.
- `page()` in `render.go` logs a session it could not read --
  `hhttp.ErrSubjectUnresolved` when the resolver failed,
  `hhttp.ErrNoSubjectResolver` when a session holds roles and none is
  installed, a store that is down -- and still draws the visitor's header. No
  session and an ended one stay silent.
- `/auth/password/update` and `/auth/verify/confirm` answer
  `onetime.ErrTooManyFailures` with "too many attempts, try again later"
  instead of "that code is not valid": once the address has spent its wrong
  codes, a new code is refused too until the window closes.

### What a republish does on its own

Nothing: the four files live in the controllers, which a plain `auth` keeps.
Publish with `auth --force`, which carries the custom blocks of
`LoginController.go` and `LoginController_handlers.go` over, or copy
`subjectAccount`, `sessionSubject`, `page` and `codeRefused` by hand.

### The resolver

The published files compile against hesape v0.76.0, the release skeleton
v0.37.0 requires; `onetime.ErrTooManyFailures` is from it. Moving to hesape
v0.77.0, which this release is tested against, means installing the session
resolver in the provider, from `User.Subject`, as hesape's UPGRADE.md for
v0.77.0 shows. Without it, `Rotate` refuses to sign in an account with roles,
and the sign-in answers 500 with `hhttp.ErrNoSubjectResolver` in the log.

---

## v0.26.0 — the code forms are limited and timed, and second-factor secrets are not cached

Three changes in the published controllers, all security fixes:

- `/auth/password/email`, `/auth/password/update`, `/auth/verify/confirm` and
  `/auth/verify/resend` count every submission per address typed (ten an hour)
  and per client (thirty per fifteen minutes), before anything is looked up. A
  code allows five tries and every new code started its own count, so the reset
  allowed thousands of guesses a day against one account.
- The reset request and the verification resend answer after the same second
  whether the address has an account or not; the time no longer says which.
- The two-factor setup screen and the recovery codes go out with
  `Cache-Control: no-store, private`.

### What a republish does on its own

Nothing: all three live in the controllers, which a plain `auth` keeps. A
project that wants them publishes with `auth --force`, which carries the custom
blocks of `LoginController.go` and `LoginController_handlers.go` over, or copies
the functions by hand.

### `authui.New` takes the rate limiter

`New` has a seventh argument, the `*cache.RateLimiter` the forms are counted
in, after the mailer. After `auth --force`, add `drivers.Limiter` to the call in
`bootstrap/app.go`:

```go
authui.New(app.Users, app.TwoFactor, app.EmailCodes, drivers.Sessions, drivers.CSRF,
    drivers.Mail, drivers.Limiter, cfg.Framework.App.Key,
    authui.FixedTenant(cfg.Auth.Tenant), cfg.Framework.Session.Secure),
```

Until it is added the project does not build, which is the point: `nil` is
accepted and counts in the process alone, so every replica would allow the
whole budget, and that is a choice to write down rather than a default to
inherit.

---

## v0.24.2 — a password stored trimmed signs in without being rewritten

`verifyTrimmedPassword` in `app/Http/Controllers/Auth/LoginController_handlers.go`
stored the password as typed through `Users.ResetPassword` when the trimmed
form matched. That rewrote the hash of anybody whose keyboard added a space at
the end: the next sign-in without the space was refused, and a
`PasswordReset` event was stored for a change nobody asked for. And
`ResetPassword` refuses an account `Hooks.Admit` refuses as a spent link,
which the handler answered with a 500 where a wrong password goes back to the
form: an oracle for a refused account's password.

It now asks `Users.VerifyCredentials` again with the trimmed form and writes
nothing. The service's throttle, timebox and `Hooks.Admit` hold the second
form as they hold the first, so a password typed with spaces and wrong both
ways counts as two attempts; every refusal on that path but a lock is the
usual message; a match signs in, into the second factor when one is on, and
leaves the stored hash as it was.

The file is kept by a plain republish. Publish it again with `auth --force`,
which carries its custom block over, or replace the function by hand with the
one in this release and change its call in `doLogin` to
`m.verifyTrimmedPassword(r, tenant, in.Email, trimmed)`; `sync` and
`hesape/hashing` leave the imports when nothing else in the file uses them.

---

## v0.24.0 — the published controllers call `hesape/auth/account`

The account rules each project carried as a copy -- `UserService`,
`TwoFactorService` and the files around them -- are a package of hesape since
v0.60.0 (`github.com/arandu-io/hesape/auth/account`). The controllers this
release publishes call it: the `Users` and `Factors` interfaces in
`LoginController.go` are the method sets of `*account.Users` and
`*account.Factors`, every method answers an `account.Account`, and no published
file imports `app/Services`.

### What a republish does on its own

`auth` without `--force` replaces `page.go`, the layout, `home.kyse.go`,
`welcome.kyse.go` and `HomeController.go`, as every release does, and keeps the
controllers. `page.go` gains `WithoutRemember` and `OffersRemember`, which an
older sign-in handler leaves at false and the screen reads as "draw the box", so
a project still on its own services keeps building and keeps its screens.

The rest of this entry is for the project that moves to the package.

### 1. Move the project to the package

Follow hesape's UPGRADE for v0.60.0; in short:

```sh
go get github.com/arandu-io/hesape@v0.60.0
```

- Delete `app/Services/UserService.go`, `app/Services/TwoFactorService.go`,
  `app/Repositories/TwoFactorRepository.go`, `app/Models/TwoFactor.go`,
  `app/Models/TwoFactorQuery.go`, `app/Models/RecoveryCodeQuery.go`,
  `app/Policies/TwoFactorPolicy.go`, and the auth event names in
  `app/Events/User.go`.
- Implement `account.Account` on `*models.User`: `AccountID`, `AccountTenant`,
  `AccountEmail`, `AccountName`, `PasswordHash`, `IsVerified`.
- Write `UserPolicy` over `*models.User` -- `auth.Policy[*models.User]` -- since
  the account handed to it is the pointer the users query builds, and
  `account.PolicyOf` takes a policy over a type that implements `Account`.
- Keep `database/migrations/20260828_0003_create_two_factor.go`; a new project
  registers `account.Migrations()` instead, never both.

### 2. Wire it in `bootstrap/app.go`

```go
users, err := account.NewUsers(account.Config{
	DB:     db,
	Query:  func() *model.Builder { return models.Users(db).Base() },
	Policy: account.PolicyOf(policies.UserPolicy{}),
})
factors, err := account.NewFactors(db, fw.App.Key, limitStore.GetStore(), nil, users)

authui.New(users, factors, emailCodes, sessions, csrf, mailer, fw.App.Key,
	authui.FixedTenant(cfg.Auth.Tenant), cfg.Framework.Session.Secure)
controllers.NewHomeController(users, cfg.Auth.Tenant)
```

The signatures of `authui.New` and `NewHomeController` did not change; what is
passed to them did. Anything else that took `*services.UserService` -- personal
access tokens, a seeder -- takes `*account.Users`, and `EnsureUser` and
`EnsureAdmin` are `Provision`, with roles written by `Draft.Fill`.

### 3. Republish the controllers

```sh
go run github.com/arandu-io/ui@v0.24.0 auth --force
```

replaces `LoginController.go`, `LoginController_handlers.go`,
`RegisterController.go`, `PasswordController.go`, `TwoFactorController.go` and
`render.go`, carrying what is inside each `arandu:begin custom` block.
`auth --views --force` also writes the sign-in screen that asks
`OffersRemember()`.

A project that edited those files outside their custom blocks makes the same
change by hand instead. The mapping:

| before | after |
| --- | --- |
| `(models.User, error)` and `(models.User, bool, error)` in `Users` | `(account.Account, error)` and `(account.Account, bool, error)` |
| `Register(ctx, tenant, name, email, password)` | `Register(ctx, tenant, account.Registration{Name: …, Email: …, Password: …})` |
| `return models.User{}, err` | `return nil, err` |
| `u.ID`, `u.TenantID`, `u.Email`, `u.Name` | `u.AccountID()`, `u.AccountTenant()`, `u.AccountEmail()`, `u.AccountName()` |
| `u.Password`, `u.Verified()` | `u.PasswordHash()`, `u.IsVerified()` |
| `u.PasswordFingerprint()` | `account.Fingerprint(u.PasswordHash())` |
| `services.NormalizeEmail`, `services.ErrEmailTaken` | `account.NormalizeEmail`, `account.ErrEmailTaken` |
| `subjectOf(u)` reading `u.Roles` | `subjectOf(u)` asserting `u.(*models.User)` for the roles |
| `services.UserService`, `services.TwoFactorService` | `*account.Users`, `*account.Factors` |

Codes in flight survive the move: the skeleton's `PasswordFingerprint` is the
function `account.Fingerprint` is, so a reset code or a pending two-factor
sign-in issued before the upgrade is accepted after it. A project whose own
fingerprint differed sees those codes refused once, and the person asks for
another.

### 4. A sign-up form of your own: `WithRegistration`

The per-project registration variants -- a `WithPersonalRegistration`, a
`WithPersonRegistration`, a `RegisterPerson` or `RegisterPersonal` method on the
user service, each with a copied `doRegister` -- are one option now:

```go
authui.New(...).WithRegistration(func(ctx *hhttp.Context, r account.Registration) (account.Registration, error) {
	var in struct {
		CPF    string `form:"cpf"`
		Mobile string `form:"mobile"`
	}
	if err := ctx.Bind(&in); err != nil {
		return r, err
	}
	if in.CPF == "" {
		return r, validation.Errors{"cpf": {"type your CPF"}}
	}
	r.Extra = person{CPF: in.CPF, Mobile: in.Mobile}
	return r, nil
})
```

and the columns are written by the account rules' hook:

```go
account.Hooks{Prepare: func(ctx context.Context, r *account.Registration, u account.Account) error {
	p, ok := r.Extra.(person)
	if !ok {
		return nil
	}
	if r.Name == "" {
		r.Name = p.CPF // or whatever the form without a name calls the account
	}
	return u.ForceFill(map[string]any{"cpf": p.CPF, "mobile": p.Mobile})
}}
```

The kit keeps binding and checking the address and the password; with a reader
set it no longer requires the name, which the reader or `Prepare` provides. A
refusal from either is `validation.Errors` and is drawn beside the kit's own.
What is created beside the account -- an organization, a membership -- is
`Hooks.AfterCreate`, in the same transaction. The extra inputs are drawn by the
project's own `register.kyse.go`.

### 5. Remember-me: `WithRemember`

`.WithRemember(false)` takes the box off the sign-in screen and ignores a
ticked one, so every sign-in ends with the browser. A project that carried its
own `WithRemember(enabled bool)` replaces it with the kit's, which means the
same.

An option to require a second factor of administrators was not taken into the
kit: what an administrator is, and the middleware that holds the console shut,
are the application's, and roles belong to `hyz-is/arandu-permission`. It stays
in the project, beside the kit's controllers.

### What the package does not do

`account.Users.Register` refuses a registration without a password, so
`registrationAsks = NoPassword` in `RegisterController.go` needs a `Users` of the
project's own in front of the package's.

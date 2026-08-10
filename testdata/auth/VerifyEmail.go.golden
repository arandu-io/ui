package mail

import (
	"github.com/arandu-io/framework/mail"
)

// VerifyEmail carries the link that confirms an address.
//
// It is the first message a new account receives, which makes it the one worth
// getting right: a verification mail that lands in spam is a registration that
// never finishes, and the person has no way to tell whether the account exists.
//
// Two things here are about that and not about the code. The subject says what
// to do rather than announcing the product, and the message has a text part --
// an HTML-only message scores worse with every filter that reads them.
type VerifyEmail struct {
	// Name is who the message greets. A first name, or empty for nobody.
	Name string
	// Link is the signed, expiring address that confirms the account.
	Link string
	// BrandName is what the message is signed with. It comes from the
	// configuration, like the name in the page header: an application with two
	// names is one nobody recognises in an inbox.
	//
	// It is a field and not a literal in the view because the view is published
	// by a starter kit. A word typed into the markup here is the kit's word, and
	// it went out on the verification mail of every project that ran the
	// command -- branded with the name of the framework rather than with theirs.
	BrandName string
}

// Envelope is who it is from and what it says it is.
//
// From is left empty on purpose: the Mailer fills in the application's address,
// so the one place it is configured is config/mail.go. A message that names its
// own sender is one that keeps sending from the old domain after a migration.
func (m VerifyEmail) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Confirm your email address",

		// arandu:begin custom
		// The tag is how a provider's dashboard tells these apart from anything
		// else this application sends -- which matters the first time
		// deliverability drops and the question is "of what".
		Tags: []string{"verify-email"},
		// arandu:end custom
	}
}

// Content is what the body is made of.
//
// Both parts render from this struct, so a field that does not exist is a
// compile error at the line of the .kyse.go rather than a blank space in
// somebody's inbox.
func (m VerifyEmail) Content() mail.Content {
	return mail.Content{
		View:     "mail.verify-email",
		TextView: "mail.verify-email-text",
		Data:     m,
	}
}

// Greeting is the name, or a word that works without one.
//
// A method on the mailable rather than a branch in each of the two views: the
// HTML part and the text part greet the same person, and two @if blocks are two
// chances for one of them to say "Hello ,".
func (m VerifyEmail) Greeting() string {
	if m.Name == "" {
		return "there"
	}
	return m.Name
}

// Compile-time proof that this is a mailable.
var _ mail.Mailable = VerifyEmail{}

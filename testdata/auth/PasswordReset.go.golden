package mail

import (
	"github.com/arandu-io/framework/mail"
)

// PasswordReset is one message this application sends.
//
// An Envelope and a Content, and nothing else. There is no ShouldQueue: sending
// on the queue is a job that calls Send, and having it decided by an interface
// the type implements somewhere else is how a call that sometimes blocks for two
// seconds becomes impossible to reason about from the line you are reading.
type PasswordReset struct {
	// Name is the Name this message carries.
	Name string
	// Link is the Link this message carries.
	Link string
	// BrandName is what the message is signed with, from the configuration and
	// never from a literal in the view. See VerifyEmail.BrandName: the two
	// messages an application sends have to be signed with the same name, and a
	// name typed into the markup is the starter kit's.
	BrandName string
}

// Envelope is who it is from and what it says it is.
//
// From is left empty on purpose: the Mailer fills in the application's address,
// so the one place it is configured is config/mail.go. A message that names its
// own sender is one that keeps sending from the old domain after a migration.
func (m PasswordReset) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Reset your password",

		// arandu:begin custom
		// The tag is how a provider's dashboard tells these apart from anything
		// else this application sends -- which matters the first time deliverability
		// drops and the question is "of what".
		Tags: []string{"password-reset"},
		// arandu:end custom
	}
}

// Content is what the body is made of.
//
// Two views, because a message with no plain-text part is filed as spam more
// often and shows nothing at all in a client that does not render HTML. Both
// render from this struct, so a field that does not exist is a compile error at
// the line of the .kyse.go rather than a blank space in somebody's inbox.
func (m PasswordReset) Content() mail.Content {
	return mail.Content{
		View:     "mail.password-reset",
		TextView: "mail.password-reset-text",
		Data:     m,
	}
}

// Compile-time proof that this is a mailable.
var _ mail.Mailable = PasswordReset{}

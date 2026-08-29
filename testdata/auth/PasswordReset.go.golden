package mail

import "github.com/arandu-io/framework/mail"

// PasswordReset carries a purpose-bound, single-use code.
type PasswordReset struct {
	Name      string
	Code      string
	BrandName string
}

func (m PasswordReset) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Reset your password",
		// arandu:begin custom
		Tags: []string{"password-reset"},
		// arandu:end custom
	}
}

func (m PasswordReset) Content() mail.Content {
	return mail.Content{View: "mail.password-reset", TextView: "mail.password-reset-text", Data: m}
}

var _ mail.Mailable = PasswordReset{}

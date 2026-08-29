package mail

import "github.com/arandu-io/framework/mail"

// VerifyEmail carries a purpose-bound, single-use code.
type VerifyEmail struct {
	Name      string
	Code      string
	BrandName string
}

func (m VerifyEmail) Envelope() mail.Envelope {
	return mail.Envelope{
		Subject: "Confirm your email address",
		// arandu:begin custom
		Tags: []string{"verify-email"},
		// arandu:end custom
	}
}

func (m VerifyEmail) Content() mail.Content {
	return mail.Content{View: "mail.verify-email", TextView: "mail.verify-email-text", Data: m}
}

func (m VerifyEmail) Greeting() string {
	if m.Name == "" {
		return "there"
	}
	return m.Name
}

var _ mail.Mailable = VerifyEmail{}

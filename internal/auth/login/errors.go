package login

// EmailRequiredError indicates that the calling context must provide an email or alias.
type EmailRequiredError struct {
	Prompt string
}

package identity

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

var validUsername = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,62}[a-z0-9]$`)

// Username is name in lower case, if it is 3 to 64 letters, digits, '.',
// '_' or '-', starting and ending with a letter or digit.
func Username(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !validUsername.MatchString(name) {
		return "", fmt.Errorf("a username is 3 to 64 letters, digits, '.', '_' or '-', starting and ending with a letter or digit")
	}
	return name, nil
}

// CheckPassword refuses a password that is too short or too long.
func CheckPassword(password string) error {
	switch n := utf8.RuneCountInString(password); {
	case n < MinPasswordLength:
		return fmt.Errorf("a password needs at least %d characters", MinPasswordLength)
	case len(password) > 256:
		return fmt.Errorf("a password has at most 256 bytes")
	}
	return nil
}

// Email is address trimmed, if it is empty or valid.
func Email(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", nil
	}
	if a, err := mail.ParseAddress(address); err != nil || a.Address != address {
		return "", fmt.Errorf("%q is not an email address", address)
	}
	return address, nil
}

// Name is a display or tenant name trimmed, if it is 1 to 100 characters.
func Name(what, name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > 100 {
		return "", fmt.Errorf("a %s is 1 to 100 characters", what)
	}
	return name, nil
}

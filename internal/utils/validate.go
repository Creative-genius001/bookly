package utils

import (
	"errors"
	"net/mail"
	"regexp"
	"unicode"
)

var (
	ErrInvalidEmail          = errors.New("invalid email format")
	ErrPasswordTooShort      = errors.New("password must be at least 8 characters long")
	ErrPasswordMissingLetter = errors.New("password must contain at least one letter")
	ErrPasswordMissingDigit  = errors.New("password must contain at least one number")
)

var symbolRegex = regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?~]`)

func ValidateEmail(email string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}

	var hasLetter, hasDigit bool

	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}

	if !hasLetter {
		return ErrPasswordMissingLetter
	}
	if !hasDigit {
		return ErrPasswordMissingDigit
	}
	return nil
}

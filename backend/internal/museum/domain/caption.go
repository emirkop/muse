package domain

import (
	"strings"
	"unicode/utf8"
)

const MaxCaptionBytes = 500

func NormalisedCaption(caption string) string {
	return strings.TrimSpace(caption)
}

func ValidateCaption(caption string) error {
	normalised := NormalisedCaption(caption)
	if !utf8.ValidString(normalised) {
		return ErrInvalidCaption
	}
	if len(normalised) > MaxCaptionBytes {
		return ErrCaptionTooLong
	}
	return nil
}

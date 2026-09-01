package domain

import (
	"strings"
	"unicode/utf8"
)

const (
	InterimMaximumNameBytes = 200

	InterimEnforcesNameUniqueness = false

	InterimAppliesProfanityFilter = false
)

const InterimMaximumReferenceBytes = 200

func ValidateName(name string) error {
	if !utf8.ValidString(name) {
		return ErrInvalidName
	}
	if strings.TrimSpace(name) == "" {
		return ErrNameRequired
	}
	if len(name) > InterimMaximumNameBytes {
		return ErrNameTooLong
	}
	return nil
}

func ValidateCategoryReference(categoryID string) error {
	if !utf8.ValidString(categoryID) || len(categoryID) > InterimMaximumReferenceBytes {
		return ErrInvalidCategoryReference
	}
	return nil
}

func ValidateDesignReference(designID string) error {
	if !utf8.ValidString(designID) || len(designID) > InterimMaximumReferenceBytes {
		return ErrInvalidDesignReference
	}
	return nil
}

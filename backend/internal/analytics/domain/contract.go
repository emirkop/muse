package domain

import (
	"crypto/rand"
	"fmt"
)

const (
	EventMuseumCreationStep         = "museum_creation_step"
	EventRoomCreationStep           = "room_creation_step"
	EventCollectionRoomCreationStep = "collection_room_creation_step"
	EventCatalogSearchPerformed     = "catalog_search_performed"
	EventCatalogSearchOutcome       = "catalog_search_outcome"
	EventItemAddRefused             = "item_add_refused"
	EventCapacityUpgradeStep        = "capacity_upgrade_step"
	EventFailureShown               = "failure_shown"
)

const NotEmittedBecauseTheDatabaseAnswersIt = "see doc comment"

const (
	PropStep           = "step"
	PropCategoryID     = "category_id"
	PropResultBucket   = "result_bucket"
	PropOutcome        = "outcome"
	PropReason         = "reason"
	PropSurface        = "surface"
	PropClassification = "classification"
	PropRetried        = "retried"
	PropRetrySucceeded = "retry_succeeded"
)

type Spec struct {
	Enums            map[string][]string
	Bools            []string
	AllowsCategoryID bool
	ServerOnly       bool
}

var Registry = map[string]Spec{
	EventMuseumCreationStep: {
		Enums: map[string][]string{
			PropStep: {"style_list_shown", "style_previewed", "style_confirmed"},
		},
	},
	EventRoomCreationStep: {
		Enums: map[string][]string{
			PropStep: {"name_entered", "variant_list_shown", "variant_previewed", "variant_confirmed"},
		},
	},
	EventCollectionRoomCreationStep: {
		Enums: map[string][]string{
			PropStep: {"category_list_shown", "category_chosen", "name_entered", "create_submitted"},
		},
		AllowsCategoryID: true,
	},
	EventCatalogSearchPerformed: {
		Enums: map[string][]string{
			PropResultBucket: {"none", "few", "some", "many"},
		},
		AllowsCategoryID: true,
		ServerOnly:       true,
	},
	EventCatalogSearchOutcome: {
		Enums: map[string][]string{
			PropOutcome: {"selected", "abandoned"},
		},
		AllowsCategoryID: true,
	},
	EventItemAddRefused: {
		Enums: map[string][]string{
			PropReason: {
				"model_not_placeable", "tier_capacity_reached", "item_capacity_reached",
				"design_layout_unavailable", "slot_not_available", "room_not_found",
			},
		},
		ServerOnly: true,
	},
	EventCapacityUpgradeStep: {
		Enums: map[string][]string{
			PropStep: {"capacity_screen_shown", "purchase_started", "purchase_failed"},
		},
	},
	EventFailureShown: {
		Enums: map[string][]string{
			PropSurface: {
				"authentication", "profile", "avatar_selection", "museum_entry", "room_list",
				"style_selection", "variant_selection", "room_entry", "photo_upload",
				"collection_room_list", "collection_room_creation", "collection_design_selection",
				"catalog_search", "collection_item_add", "sharing", "music", "capacity", "launch",
			},
			PropClassification: {"offline", "unreachable", "server", "content"},
		},
		Bools: []string{PropRetried, PropRetrySucceeded},
	},
}

func Names() []string {
	names := make([]string, 0, len(Registry))
	for name := range Registry {
		names = append(names, name)
	}
	return names
}

type ValidationError struct {
	Field string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("analytics: invalid or unpermitted %s", e.Field)
}

type Event struct {
	UUID       string
	Name       string
	Step       *string
	CategoryID *string
	Result     *string
	Outcome    *string
	Reason     *string
	Surface    *string
	Class      *string
	Retried    *bool
	RetryOK    *bool
}

type Draft struct {
	UUID       string
	Name       string
	Step       *string
	CategoryID *string
	Result     *string
	Outcome    *string
	Reason     *string
	Surface    *string
	Class      *string
	Retried    *bool
	RetryOK    *bool
}

func Validate(d Draft, fromClient bool) (Event, error) {
	spec, ok := Registry[d.Name]
	if !ok {
		return Event{}, ValidationError{Field: "event name"}
	}
	if fromClient && spec.ServerOnly {
		return Event{}, ValidationError{Field: "event name"}
	}
	if !isUUID(d.UUID) {
		return Event{}, ValidationError{Field: "event_uuid"}
	}

	present := map[string]*string{
		PropStep:           d.Step,
		PropResultBucket:   d.Result,
		PropOutcome:        d.Outcome,
		PropReason:         d.Reason,
		PropSurface:        d.Surface,
		PropClassification: d.Class,
	}
	for prop, value := range present {
		allowed, permitted := spec.Enums[prop]
		if value == nil {
			if permitted {
				return Event{}, ValidationError{Field: prop}
			}
			continue
		}
		if !permitted || !contains(allowed, *value) {
			return Event{}, ValidationError{Field: prop}
		}
	}

	bools := map[string]*bool{PropRetried: d.Retried, PropRetrySucceeded: d.RetryOK}
	for prop, value := range bools {
		required := contains(spec.Bools, prop)
		if (value == nil) != !required {
			return Event{}, ValidationError{Field: prop}
		}
	}

	if d.CategoryID != nil {
		if !spec.AllowsCategoryID || !isPlausibleCategoryID(*d.CategoryID) {
			return Event{}, ValidationError{Field: PropCategoryID}
		}
	}

	return Event{
		UUID: d.UUID, Name: d.Name, Step: d.Step, CategoryID: d.CategoryID,
		Result: d.Result, Outcome: d.Outcome, Reason: d.Reason,
		Surface: d.Surface, Class: d.Class, Retried: d.Retried, RetryOK: d.RetryOK,
	}, nil
}

func ResultBucket(count int) string {
	switch {
	case count <= 0:
		return "none"
	case count <= 5:
		return "few"
	case count <= 25:
		return "some"
	default:
		return "many"
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

func isPlausibleCategoryID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ':'
		if !ok {
			return false
		}
	}
	return true
}

func NewEventUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

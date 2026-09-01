package domain

import "testing"

func TestValidate_UnknownEventNameIsRefused(t *testing.T) {
	for _, name := range []string{"", "unknown_event", "MUSEUM_CREATION_STEP", "onboarding_step_reached"} {
		if _, err := Validate(Draft{UUID: sampleUUID, Name: name}, true); err == nil {
			t.Errorf("event name %q was accepted; the registry is the whole contract", name)
		}
	}
}

func TestValidate_TheDroppedOnboardingEventHasNoRepresentation(t *testing.T) {
	if _, ok := Registry["onboarding_step_reached"]; ok {
		t.Fatal("onboarding_step_reached must not exist: forbids the pre-auth half and accounts.avatar_id answers the rest")
	}
}

func TestValidate_UnpermittedPropertyOnAPermittedEventIsRefused(t *testing.T) {
	surface := "room_entry"
	step := "style_list_shown"
	_, err := Validate(Draft{UUID: sampleUUID, Name: EventMuseumCreationStep, Step: &step, Surface: &surface}, true)
	if err == nil {
		t.Fatal("a property the event's spec does not name must be refused, not ignored")
	}
}

func TestValidate_UnpermittedEnumValueIsRefused(t *testing.T) {
	bad := []string{"", "Style_List_Shown", "style_list_shown ", "watches", "a room called home"}
	for _, value := range bad {
		v := value
		if _, err := Validate(Draft{UUID: sampleUUID, Name: EventMuseumCreationStep, Step: &v}, true); err == nil {
			t.Errorf("step %q was accepted", value)
		}
	}
}

func TestValidate_MissingRequiredPropertyIsRefused(t *testing.T) {
	if _, err := Validate(Draft{UUID: sampleUUID, Name: EventMuseumCreationStep}, true); err == nil {
		t.Fatal("an event with no step is not a countable event")
	}
	surface, class := "room_entry", "offline"
	retried := true
	if _, err := Validate(Draft{
		UUID: sampleUUID, Name: EventFailureShown, Surface: &surface, Class: &class, Retried: &retried,
	}, true); err == nil {
		t.Fatal("failure_shown without retry_succeeded must be refused")
	}
}

func TestValidate_ServerOnlyEventsAreRefusedFromAClient(t *testing.T) {
	category, bucket := "category_watches", "few"
	if _, err := Validate(Draft{
		UUID: sampleUUID, Name: EventCatalogSearchPerformed, CategoryID: &category, Result: &bucket,
	}, true); err == nil {
		t.Fatal("a client must not be able to emit catalog_search_performed — the server observes it, and a client could forge counts")
	}
	if _, err := Validate(Draft{
		UUID: sampleUUID, Name: EventCatalogSearchPerformed, CategoryID: &category, Result: &bucket,
	}, false); err != nil {
		t.Fatalf("server-side emission of the same event: %v", err)
	}

	reason := "item_capacity_reached"
	if _, err := Validate(Draft{UUID: sampleUUID, Name: EventItemAddRefused, Reason: &reason}, true); err == nil {
		t.Fatal("a client must not be able to emit item_add_refused")
	}
}

func TestValidate_CategoryIDMustBeAPlatformIDNotText(t *testing.T) {
	step := "category_chosen"
	for _, value := range []string{"", "Watches", "my watches", "a; DROP TABLE", "café", string(make([]byte, 65))} {
		v := value
		_, err := Validate(Draft{
			UUID: sampleUUID, Name: EventCollectionRoomCreationStep, Step: &step, CategoryID: &v,
		}, true)
		if err == nil {
			t.Errorf("category_id %q was accepted; the whitelist is what keeps user text out", value)
		}
	}
	good := "category_hot_wheels"
	if _, err := Validate(Draft{
		UUID: sampleUUID, Name: EventCollectionRoomCreationStep, Step: &step, CategoryID: &good,
	}, true); err != nil {
		t.Fatalf("a real category id: %v", err)
	}
}

func TestValidate_CategoryIDIsRefusedWhereItIsNotPartOfTheQuestion(t *testing.T) {
	step, category := "style_list_shown", "category_watches"
	if _, err := Validate(Draft{
		UUID: sampleUUID, Name: EventMuseumCreationStep, Step: &step, CategoryID: &category,
	}, true); err == nil {
		t.Fatal("museum_creation_step has no category; the property must be refused there")
	}
}

func TestValidate_EventUUIDMustBeAUUID(t *testing.T) {
	step := "style_list_shown"
	for _, id := range []string{"", "not-a-uuid", "12345678901234567890123456789012345", sampleUUID + "0"} {
		if _, err := Validate(Draft{UUID: id, Name: EventMuseumCreationStep, Step: &step}, true); err == nil {
			t.Errorf("event_uuid %q was accepted", id)
		}
	}
}

func TestValidationError_NeverEchoesTheOffendingValue(t *testing.T) {
	secret := "a room named after my child"
	step := secret
	_, err := Validate(Draft{UUID: sampleUUID, Name: EventMuseumCreationStep, Step: &step}, true)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if got := err.Error(); contains([]string{got}, secret) || len(got) > 80 {
		t.Fatalf("the error must not carry the value: %q", got)
	}
}

func TestNewEventUUID_IsRandomAndValid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewEventUUID()
		if !isUUID(id) {
			t.Fatalf("generated an invalid uuid: %q", id)
		}
		if seen[id] {
			t.Fatal("generated a duplicate uuid; the key must be random per event, never derived from a device")
		}
		seen[id] = true
	}
}

func TestResultBucket_NeverExposesAnExactCount(t *testing.T) {
	cases := map[int]string{-1: "none", 0: "none", 1: "few", 5: "few", 6: "some", 25: "some", 26: "many", 10_000: "many"}
	for count, want := range cases {
		if got := ResultBucket(count); got != want {
			t.Errorf("ResultBucket(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestRegistry_CarriesOnlyTheNineTypedProperties(t *testing.T) {
	allowed := map[string]bool{
		PropStep: true, PropCategoryID: true, PropResultBucket: true, PropOutcome: true,
		PropReason: true, PropSurface: true, PropClassification: true,
		PropRetried: true, PropRetrySucceeded: true,
	}
	for name, spec := range Registry {
		for prop := range spec.Enums {
			if !allowed[prop] {
				t.Errorf("%s declares unknown property %q", name, prop)
			}
			if len(spec.Enums[prop]) == 0 {
				t.Errorf("%s property %q has no closed value set — that is a free-text field", name, prop)
			}
		}
		for _, prop := range spec.Bools {
			if !allowed[prop] {
				t.Errorf("%s declares unknown boolean %q", name, prop)
			}
		}
	}
	if len(Registry) != 8 {
		t.Fatalf("the registry holds %d events; the documented set is 8 (7 product events plus failure_shown)", len(Registry))
	}
}

const sampleUUID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

func TestNames_ReportsExactlyTheRegistry(t *testing.T) {
	names := Names()
	if len(names) != len(Registry) {
		t.Fatalf("Names() returned %d, the registry holds %d", len(names), len(Registry))
	}
	for _, name := range names {
		if _, ok := Registry[name]; !ok {
			t.Errorf("Names() reported %q, which the registry does not hold", name)
		}
	}
	reported := map[string]bool{}
	for _, name := range names {
		reported[name] = true
	}
	for name := range Registry {
		if !reported[name] {
			t.Errorf("%q is in the registry but not reported by Names()", name)
		}
	}
}

func TestIsUUID_HyphenPositionsAreStructural(t *testing.T) {
	if !isUUID("3f2504e0-4f89-41d3-9a0c-0305e82c3301") {
		t.Fatal("a canonical UUID was refused")
	}
	if !isUUID("3F2504E0-4F89-41D3-9A0C-0305E82C3301") {
		t.Error("uppercase hex is still a UUID")
	}
	for _, bad := range []string{
		"3f2504e04f8941d39a0c0305e82c3301",
		"3f2504e0-4f89-41d3-9a0c0305-e82c3301",
		"3f2504e0_4f89_41d3_9a0c_0305e82c3301",
		"3f2504e0-4f89-41d3-9a0c-0305e82c330g",
		"3f2504e0-4f89-41d3-9a0c-0305e82c330 ",
	} {
		if isUUID(bad) {
			t.Errorf("%q was accepted as a UUID", bad)
		}
	}
}

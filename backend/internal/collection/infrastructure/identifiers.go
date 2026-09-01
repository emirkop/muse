package infrastructure

import "regexp"

var plausibleUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func IsPlausibleUUID(id string) bool {
	return plausibleUUID.MatchString(id)
}

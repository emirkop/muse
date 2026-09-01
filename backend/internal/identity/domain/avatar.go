package domain

type AvatarID string

const (
	Avatar1 AvatarID = "avatar_1"
	Avatar2 AvatarID = "avatar_2"
	Avatar3 AvatarID = "avatar_3"
	Avatar4 AvatarID = "avatar_4"
	Avatar5 AvatarID = "avatar_5"
)

var AvailableAvatarIDs = []AvatarID{Avatar1, Avatar2, Avatar3, Avatar4, Avatar5}

func IsValidAvatarID(id AvatarID) bool {
	for _, avatar := range AvailableAvatarIDs {
		if avatar == id {
			return true
		}
	}
	return false
}

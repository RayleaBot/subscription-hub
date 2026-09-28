package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func CurrentTargetName(event *rayleabot.EventContext) string {
	name := strings.TrimSpace(event.Event.Target.Name)
	onebot := MapValue(event.Event.Payload["onebot"])
	sender := MapValue(onebot["sender"])
	// Group names arrive only as Target.Name: payload.onebot is a closed
	// projection and never carries group_name.
	if name == "" && NormalizedTargetType(event.Event.Target.Type) == "private" {
		name = FirstText(event.Event.Actor.Nickname, sender["nickname"])
	}
	if name == event.Event.Target.ID {
		return ""
	}
	return name
}

// ActorID returns the sender of the current event, falling back to the
// OneBot payload when the protocol actor frame is absent.
func ActorID(event *rayleabot.EventContext) string {
	onebot := MapValue(event.Event.Payload["onebot"])
	sender := MapValue(onebot["sender"])
	return FirstText(event.Event.Actor.ID, sender["user_id"], onebot["user_id"])
}

// ActorIsSuperAdmin reports whether the current event was sent by one of the
// host-declared super admins.
func ActorIsSuperAdmin(event *rayleabot.EventContext) bool {
	id := ActorID(event)
	if id == "" {
		return false
	}
	for _, superAdmin := range event.SuperAdmins {
		if strings.TrimSpace(superAdmin) == id {
			return true
		}
	}
	return false
}

func MergeSubscriber(items []Subscriber, event *rayleabot.EventContext) []Subscriber {
	onebot := MapValue(event.Event.Payload["onebot"])
	sender := MapValue(onebot["sender"])
	id := ActorID(event)
	if id == "" {
		return items
	}
	actorRole := strings.ToLower(strings.TrimSpace(event.Event.Actor.Role))
	senderRole := strings.ToLower(strings.TrimSpace(StringScalar(sender["role"])))
	baseRole := strongestSubscriberBaseRole(actorRole, senderRole)
	role := strongestSubscriberRole(actorRole, senderRole)
	if ActorIsSuperAdmin(event) {
		role = "super_admin"
	}
	next := Subscriber{
		ID: id, Nickname: FirstText(event.Event.Actor.Nickname, sender["nickname"], id),
		GroupNickname: StringScalar(sender["card"]), Title: StringScalar(sender["title"]),
		BaseRole: baseRole, Role: role, RoleLabel: subscriberRoleLabel(role), AvatarURL: qqAvatarURL(id),
	}
	for index := range items {
		if items[index].ID == id {
			items[index] = mergeSubscriberIdentity(items[index], next)
			return items
		}
	}
	return append(items, next)
}

func NormalizeSubscriptionSubscribers(items []Subscription, superAdmins []string) {
	adminIDs := make(map[string]struct{}, len(superAdmins))
	for _, id := range superAdmins {
		if id = strings.TrimSpace(id); id != "" {
			adminIDs[id] = struct{}{}
		}
	}
	for itemIndex := range items {
		for subscriberIndex := range items[itemIndex].Subscribers {
			current := &items[itemIndex].Subscribers[subscriberIndex]
			current.ID = strings.TrimSpace(current.ID)
			current.Nickname = FirstText(current.Nickname, current.ID)
			current.GroupNickname = strings.TrimSpace(current.GroupNickname)
			current.Title = strings.TrimSpace(current.Title)
			current.BaseRole = strings.ToLower(strings.TrimSpace(current.BaseRole))
			current.Role = strings.ToLower(strings.TrimSpace(current.Role))
			if current.BaseRole == "super_admin" {
				current.BaseRole = ""
			}
			if current.BaseRole == "" && current.Role != "super_admin" {
				current.BaseRole = current.Role
			}
			if _, ok := adminIDs[current.ID]; ok {
				current.Role = "super_admin"
			} else if current.Role == "super_admin" {
				current.Role = FirstText(current.BaseRole, "member")
			}
			if label := subscriberRoleLabel(current.Role); label != "" {
				current.RoleLabel = label
			} else {
				current.RoleLabel = strings.TrimSpace(current.RoleLabel)
			}
			if avatar := qqAvatarURL(current.ID); avatar != "" {
				current.AvatarURL = avatar
			} else {
				current.AvatarURL = strings.TrimSpace(current.AvatarURL)
			}
		}
	}
}

func mergeSubscriberIdentity(current, incoming Subscriber) Subscriber {
	current.ID = FirstText(incoming.ID, current.ID)
	current.Nickname = FirstText(incoming.Nickname, current.Nickname, current.ID)
	current.GroupNickname = FirstText(incoming.GroupNickname, current.GroupNickname)
	current.Title = FirstText(incoming.Title, current.Title)
	current.BaseRole = FirstText(incoming.BaseRole, current.BaseRole)
	current.Role = FirstText(incoming.Role, current.Role)
	current.RoleLabel = FirstText(subscriberRoleLabel(current.Role), incoming.RoleLabel, current.RoleLabel)
	current.AvatarURL = FirstText(qqAvatarURL(current.ID), incoming.AvatarURL, current.AvatarURL)
	return current
}

func strongestSubscriberRole(values ...string) string {
	ranks := map[string]int{"member": 1, "admin": 2, "owner": 3, "super_admin": 4}
	strongest := ""
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if ranks[value] > ranks[strongest] {
			strongest = value
		}
	}
	return strongest
}

func strongestSubscriberBaseRole(values ...string) string {
	ranks := map[string]int{"member": 1, "admin": 2, "owner": 3}
	strongest := ""
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if ranks[value] > ranks[strongest] {
			strongest = value
		}
	}
	return strongest
}

func SubscriptionID(platform, uid, targetType, targetID string) string {
	value := strings.Join([]string{platform, uid, targetType, targetID}, "|")
	digest := sha256.Sum256([]byte(value))
	return platform + "-" + hex.EncodeToString(digest[:8])
}

func NormalizedTargetType(value string) string {
	if strings.TrimSpace(value) == "private" {
		return "private"
	}
	return "group"
}

func subscribersText(item Subscription) string {
	names := make([]string, 0, len(item.Subscribers))
	for _, subscriber := range item.Subscribers {
		if name := FirstText(subscriber.Nickname, subscriber.ID); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "未记录"
	}
	return strings.Join(names, "、")
}

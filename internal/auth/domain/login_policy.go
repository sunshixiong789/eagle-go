package domain

import "slices"

// LoginProfile 是登录时可同步的账号资料，不包含第三方身份归属。
type LoginProfile struct {
	DisplayName string
	AvatarURL   string
}

// MergeLoginProfile 只补全空昵称，使用非空的第三方头像更新账号，避免后续登录覆盖已设置的昵称。
func MergeLoginProfile(current, incoming LoginProfile) LoginProfile {
	if current.DisplayName == "" {
		current.DisplayName = incoming.DisplayName
	}
	if incoming.AvatarURL != "" {
		current.AvatarURL = incoming.AvatarURL
	}
	return current
}

// InitialAudienceRoles 为首次加入 audience 且没有角色的账号分配 user；已有角色保持不变。
// 只用于首次初始化，已初始化的空授权不得调用此规则恢复默认角色。
func InitialAudienceRoles(existing []string) []string {
	if len(existing) == 0 {
		return []string{"user"}
	}
	return slices.Clone(existing)
}

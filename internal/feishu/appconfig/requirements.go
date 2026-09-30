package appconfig

// RequiredScopes is the authoritative list of Feishu scopes this binary's
// feature set needs. It is intentionally self-contained: the startup
// self-heal flow compares it against the platform-side configuration and
// repairs any drift, so no external document is involved at runtime.
//
// Keep entries grouped by feature area; every scope added here will be
// auto-applied (and, where missing, requested through an auth link) for
// every bot running this binary.
func RequiredScopes() []string {
	return []string{
		// 消息收发:发送/回复消息与卡片、读取消息内容。
		"im:message:send_as_bot",
		"im:message:readonly",
		// 富媒体:上传/下载图片与文件。
		"im:resource",
		// 表情回执:排队/THINKING/👎(取消排队入口)。
		"im:message.reactions:write_only",
		// 群聊:群信息、接收单聊/群聊消息(含非 @ 消息,敏感权限)。
		"im:chat:readonly",
		"im:message.p2p_msg:readonly",
		"im:message.group_at_msg:readonly",
		"im:message.group_msg",
		// 群公告状态区(多 Bot 状态)。
		"im:chat.announcement:read",
		"im:chat.announcement:write_only",
		// 本地文件链接(云空间):上传、目录管理、元数据、协作者授权。
		"drive:drive",
		"drive:drive.metadata:readonly",
		"docs:permission.member:create",
		// 加急通知(权限错误卡片等)。
		"im:message.urgent",
		// 启动自愈自身的前置能力:改配置与发布版本。
		PatchScope,
	}
}

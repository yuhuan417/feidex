package state

import "testing"

func TestDeleteFrontendCardNotificationsByCollapseKey(t *testing.T) {
	store := openTestStore(t)

	if err := store.AppendFrontendCardNotification("frontend-a", FrontendCardNotification{
		Kind:        "feishu_app_config_heal",
		CollapseKey: "feishu_app_config_heal",
		Title:       "需要飞书授权",
		Color:       "red",
		Body:        "缺少 application:application:patch",
	}); err != nil {
		t.Fatalf("AppendFrontendCardNotification() error = %v", err)
	}
	if err := store.AppendFrontendCardNotification("frontend-a", FrontendCardNotification{
		Kind:  "other",
		Title: "其它通知",
		Body:  "保留",
	}); err != nil {
		t.Fatalf("AppendFrontendCardNotification(other) error = %v", err)
	}

	if err := store.DeleteFrontendCardNotificationsByCollapseKey("frontend-a", "feishu_app_config_heal"); err != nil {
		t.Fatalf("DeleteFrontendCardNotificationsByCollapseKey() error = %v", err)
	}
	remaining := store.FrontendCardNotifications("frontend-a")
	if len(remaining) != 1 || remaining[0].Title != "其它通知" {
		t.Fatalf("remaining notifications = %+v", remaining)
	}

	reopened, err := Open(store.path)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	if remaining := reopened.FrontendCardNotifications("frontend-a"); len(remaining) != 1 || remaining[0].Title != "其它通知" {
		t.Fatalf("reopened notifications = %+v", remaining)
	}

	if err := store.DeleteFrontendCardNotificationsByCollapseKey("frontend-a", "missing"); err != nil {
		t.Fatalf("DeleteFrontendCardNotificationsByCollapseKey(missing) error = %v", err)
	}
	if err := store.DeleteFrontendCardNotificationsByCollapseKey("frontend-a", ""); err != nil {
		t.Fatalf("DeleteFrontendCardNotificationsByCollapseKey(empty) error = %v", err)
	}
	if remaining := store.FrontendCardNotifications("frontend-a"); len(remaining) != 1 {
		t.Fatalf("notifications after no-op deletes = %+v", remaining)
	}
}

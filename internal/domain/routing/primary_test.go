package routing

import "testing"

func TestParseGroupPrimaryAssignment(t *testing.T) {
	assignment, ok := ParseGroupPrimaryAssignment("/primary on", []string{"bot-a"})
	if !ok || assignment.TargetBotOpenID != "bot-a" {
		t.Fatalf("assignment = %#v, ok=%v", assignment, ok)
	}
}

func TestParseGroupPrimaryAssignmentRejectsAmbiguousMentions(t *testing.T) {
	if _, ok := ParseGroupPrimaryAssignment("/primary on", []string{"bot-a", "bot-b"}); ok {
		t.Fatal("multiple mentions must not produce an assignment")
	}
}

func TestPrimaryParsing(t *testing.T) {
	if !ParsePrimaryOnCommand("@bot /primary on") {
		t.Fatal("expected mentioned /primary on command")
	}
	if ParsePrimaryOnCommand("/primary off") {
		t.Fatal("/primary off must not be treated as assignment")
	}
	if !ParseEmptyBotMention("@bot") || ParseEmptyBotMention("@bot hello") {
		t.Fatal("unexpected empty mention parsing")
	}
}

func TestStaleAssignment(t *testing.T) {
	if !StaleAssignment("msg-new", 200, "msg-old", 100) {
		t.Fatal("older assignment should be stale")
	}
	if StaleAssignment("msg-new", 200, "msg-current", 300) {
		t.Fatal("newer assignment should not be stale")
	}
}

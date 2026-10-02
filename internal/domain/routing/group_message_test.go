package routing

import "testing"

func TestAcceptGroupMessage(t *testing.T) {
	tests := []struct {
		name string
		in   GroupMessageInput
		want bool
	}{
		{name: "mention all", in: GroupMessageInput{MentionAll: true}, want: true},
		{name: "mention self", in: GroupMessageInput{MentionedSelf: true, MentionedAny: true}, want: true},
		{name: "other mention", in: GroupMessageInput{MentionedAny: true, IsPrimary: true}, want: false},
		{name: "owned reply", in: GroupMessageInput{InReplyChain: true, HasLocalLink: true}, want: true},
		{name: "foreign reply", in: GroupMessageInput{InReplyChain: true, IsPrimary: true}, want: false},
		{name: "primary root", in: GroupMessageInput{IsPrimary: true}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AcceptGroupMessage(test.in); got != test.want {
				t.Fatalf("AcceptGroupMessage(%+v) = %t, want %t", test.in, got, test.want)
			}
		})
	}
}

func TestProbeGroupPrimary(t *testing.T) {
	if !ProbeGroupPrimary(GroupMessageInput{}) {
		t.Fatal("uninitialized root message should probe primary state")
	}
	if ProbeGroupPrimary(GroupMessageInput{HasPrimaryState: true}) {
		t.Fatal("initialized group should not probe primary state")
	}
	if !ProbeGroupPrimary(GroupMessageInput{MentionedSelf: true, InReplyChain: true}) {
		t.Fatal("mentioning self should probe even from a reply")
	}
	if ProbeGroupPrimary(GroupMessageInput{MentionedAny: true}) {
		t.Fatal("mentioning another target should not probe")
	}
}

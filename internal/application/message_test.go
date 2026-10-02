package application

import "testing"

func TestClassifyMessageRoutePrecedence(t *testing.T) {
	tests := []struct {
		name  string
		input MessageRouteInput
		want  MessageRoute
	}{
		{"server pending wins", MessageRouteInput{PendingServerText: true, PendingRootText: true}, MessageRoutePendingServerText},
		{"command bypasses pending text", MessageRouteInput{StartsCommand: true, LocalCommand: true, PendingRootText: true}, MessageRouteLocalCommand},
		{"local command", MessageRouteInput{StartsCommand: true, LocalCommand: true}, MessageRouteLocalCommand},
		{"staged image before empty", MessageRouteInput{TextEmpty: true, StageImages: true}, MessageRouteStageImages},
		{"empty", MessageRouteInput{TextEmpty: true}, MessageRouteNoop},
		{"normal submission", MessageRouteInput{HasAttachments: true}, MessageRouteSteerOrQueue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyMessageRoute(test.input); got != test.want {
				t.Fatalf("route = %v, want %v", got, test.want)
			}
		})
	}
}

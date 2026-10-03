package approval

import "feidex/internal/application"

// PresentationForEvent builds approval content from a semantic backend event.
// The caller merges item metadata and supplies the workspace without allowing
// presentation to read or write live session state.
func PresentationForEvent(event application.BackendEvent, merge func(Presentation) Presentation, cwd string) Presentation {
	if event.Approval == nil {
		return Presentation{}
	}
	value := *event.Approval
	p := Presentation{Kind: NormalizeKind(value.Kind), ThreadID: event.ThreadID, TurnID: event.TurnID, ItemID: value.ItemID, Payload: RequestPayload{Request: value.Request}}
	if merge != nil {
		p = merge(p)
	}
	switch p.Kind {
	case KindCommand:
		p.Body = RenderCommandBody(p.Payload.Request)
	case KindFile:
		p.Body = RenderFileBodyWithWorkspace(p.Payload.Request, cwd)
	case KindPermissions:
		if permissions, ok := p.Payload.Request["permissions"].(map[string]any); ok && len(p.Payload.Permissions) == 0 {
			p.Payload.Permissions = CloneJSONMap(permissions)
		}
		p.Body = RenderPermissionsApprovalBody(p.Payload.Request)
	}
	p.Payload.Body = p.Body
	return p
}

package conversation

// CloneSession creates a deep copy of a session.
func CloneSession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.ActiveOperations = append([]SessionActiveOperation(nil), sess.ActiveOperations...)
	cp.StagedImages = append([]SessionStagedImage(nil), sess.StagedImages...)
	if sess.ActiveThreadCollaborationMode != nil {
		mode := *sess.ActiveThreadCollaborationMode
		if sess.ActiveThreadCollaborationMode.DeveloperInstructions != nil {
			value := *sess.ActiveThreadCollaborationMode.DeveloperInstructions
			mode.DeveloperInstructions = &value
		}
		cp.ActiveThreadCollaborationMode = &mode
	}
	if len(sess.BackendThreads) > 0 {
		cp.BackendThreads = make(map[string]SessionBackendThread, len(sess.BackendThreads))
		for key, value := range sess.BackendThreads {
			if value.CollaborationMode != nil {
				mode := *value.CollaborationMode
				if value.CollaborationMode.DeveloperInstructions != nil {
					instructions := *value.CollaborationMode.DeveloperInstructions
					mode.DeveloperInstructions = &instructions
				}
				value.CollaborationMode = &mode
			}
			cp.BackendThreads[key] = value
		}
	}
	return &cp
}

package app

func (s runtimeStateService) beginFrontendMessageTraffic() {
	if s.app == nil {
		return
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	s.app.frontendMessageTraffic++
}

func (s runtimeStateService) finishFrontendMessageTraffic() {
	if s.app == nil {
		return
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	if s.app.frontendMessageTraffic > 0 {
		s.app.frontendMessageTraffic--
	}
}

func (s runtimeStateService) frontendMessageTrafficCount() int {
	if s.app == nil {
		return 0
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	return s.app.frontendMessageTraffic
}

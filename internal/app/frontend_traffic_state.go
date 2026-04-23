package app

func (a *App) beginFrontendMessageTraffic() {
	if a == nil {
		return
	}
	a.frontendTrafficMu.Lock()
	defer a.frontendTrafficMu.Unlock()
	a.frontendMessageTraffic++
}

func (a *App) finishFrontendMessageTraffic() {
	if a == nil {
		return
	}
	a.frontendTrafficMu.Lock()
	defer a.frontendTrafficMu.Unlock()
	if a.frontendMessageTraffic > 0 {
		a.frontendMessageTraffic--
	}
}

func (a *App) frontendMessageTrafficCount() int {
	if a == nil {
		return 0
	}
	a.frontendTrafficMu.Lock()
	defer a.frontendTrafficMu.Unlock()
	return a.frontendMessageTraffic
}

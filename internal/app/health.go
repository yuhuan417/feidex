package app

import "time"

type healthResponse struct {
	Status  string `json:"status"`
	Backend string `json:"backend"`
	Uptime  string `json:"uptime"`
}

func (a *App) healthStatus() healthResponse {
	uptime := time.Since(a.started).Truncate(time.Second).String()
	return healthResponse{
		Status:  "ok",
		Backend: configuredBackend(a),
		Uptime:  uptime,
	}
}

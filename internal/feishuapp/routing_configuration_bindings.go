package feishuapp

import (
	"feidex/internal/application/modelconfig"
)

type modelWriteAdmission struct{ app *App }

func (s modelWriteAdmission) ModelConfigBlockedReason() string {
	return modelConfigBlockedReason(s.app)
}

func ModelWriteAdmission(a *App) modelconfig.WriteAdmission { return modelWriteAdmission{app: a} }

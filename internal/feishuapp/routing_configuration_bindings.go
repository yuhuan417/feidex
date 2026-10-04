package feishuapp

import (
	"feidex/internal/application/frontend"
	"feidex/internal/application/modelconfig"
)

type modelWriteAdmission struct{ query frontend.Query }

func (s modelWriteAdmission) ModelConfigBlockedReason() string {
	return modelConfigBlockedReason(s.query)
}

func ModelWriteAdmission(query frontend.Query) modelconfig.WriteAdmission {
	return modelWriteAdmission{query: query}
}

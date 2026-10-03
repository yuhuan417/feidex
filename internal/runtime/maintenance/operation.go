package maintenance

import (
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/runtime"
	"fmt"
)

type OperationRunner struct {
	Lifecycle *runtime.FrontendRuntime
	Service   *backendmaintenance.Service
	Executor  func(func())
}

func (r OperationRunner) Start(operation backendmaintenance.Operation) error {
	if r.Lifecycle.Run(func() { r.Service.Run(r.Lifecycle.Context(), operation) }, r.Executor) {
		return nil
	}
	err := fmt.Errorf("frontend is shutting down")
	r.Service.Reject(operation, err)
	return err
}

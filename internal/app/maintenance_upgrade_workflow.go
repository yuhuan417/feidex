package app

import appmaintenance "feidex/internal/app/maintenance"

type maintenanceUpgradeProbe = appmaintenance.UpgradeProbe
type maintenanceUpgradeWorkflow = appmaintenance.UpgradeWorkflow

func runMaintenanceUpgradeWorkflow(w maintenanceUpgradeWorkflow) {
	appmaintenance.RunUpgradeWorkflow(w)
}

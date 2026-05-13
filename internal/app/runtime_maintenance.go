package app

import (
	appmaintenance "feidex/internal/app/maintenance"
)

type runtimeMaintenanceService = appmaintenance.RuntimeMaintenanceService

var newRuntimeMaintenanceService = appmaintenance.NewRuntimeMaintenanceService

const attachmentRetention = appmaintenance.AttachmentRetention
const artifactGCTimeout = appmaintenance.ArtifactGCTimeout

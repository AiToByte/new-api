package service

import (
	"github.com/QuantumNous/new-api/model"
)

// Model metadata administration seam. Controllers validate input and render
// responses; the GORM calls live in model/ and are reached through here so
// future cross-cutting concerns (audit, cache) have a single seam — mirroring
// the subscription plan admin seam.

// GetModelMetadata returns a single model metadata record by primary key.
func GetModelMetadata(id int) (*model.Model, error) {
	return model.GetModelMetadata(id)
}

// UpdateModelStatus flips only the catalog visibility of a single model.
func UpdateModelStatus(id int, status int) error {
	return model.UpdateModelStatus(id, status)
}

// GetMetadataSyncState returns the local model/vendor maps used to diff against
// the upstream catalog. The seam supplies model.DB so controllers never
// reference the global handle directly.
func GetMetadataSyncState() (map[string]*model.Model, map[string]*model.Vendor, error) {
	return model.GetMetadataSyncState(model.DB)
}

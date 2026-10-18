package service

import (
	"github.com/QuantumNous/new-api/model"
)

// Plan administration persistence. Controllers validate input and render
// responses; the GORM calls live in model/ and are reached through here so
// future cross-cutting concerns (audit, pricing sync) have a single seam.

// CreateSubscriptionPlan persists a validated plan draft.
func CreateSubscriptionPlan(plan *model.SubscriptionPlan) error {
	return model.CreateSubscriptionPlan(plan)
}

// UpdateSubscriptionPlan persists validated admin edits onto a plan.
func UpdateSubscriptionPlan(plan *model.SubscriptionPlan) error {
	return model.UpdateSubscriptionPlan(plan)
}

// UpdateSubscriptionPlanStatus flips only the enabled flag of a plan.
func UpdateSubscriptionPlanStatus(id int, enabled bool) error {
	return model.UpdateSubscriptionPlanStatus(id, enabled)
}

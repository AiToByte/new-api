package service

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
)

// Admin user write paths. These transactions span the user table (model) and the
// authorization matrix (service/authz), so the transaction is owned here rather
// than in the controller (Phase 3.2 layering).

// applyAdminUserPermissions applies the admin permission matrix inside an open
// transaction. A root operator clearing permissions on a non-admin target clears
// the matrix; setting a matrix requires root and only applies to admin targets.
func applyAdminUserPermissions(tx *gorm.DB, userID, targetRole, operatorRole int, permissions map[string]map[string]bool) (bool, error) {
	if permissions == nil {
		if targetRole < common.RoleAdminUser && operatorRole == common.RoleRootUser {
			return true, authz.ClearUserAuthorizationInTx(tx, userID)
		}
		return false, nil
	}
	if operatorRole != common.RoleRootUser {
		return false, fmt.Errorf("only root can update admin permissions")
	}
	if targetRole < common.RoleAdminUser {
		return true, authz.ClearUserAuthorizationInTx(tx, userID)
	}
	return true, authz.SetUserPermissionsInTx(tx, userID, permissions)
}

// UpdateAdminUserWithPermissions atomically persists admin edits to a user and
// applies the admin permission matrix changes in the same transaction. It
// reports whether the authorization policy was touched; callers reload the
// policy after the transaction commits.
func UpdateAdminUserWithPermissions(user *model.User, updatePassword bool, permissions map[string]map[string]bool, operatorRole int) (bool, error) {
	authzTouched := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := user.EditWithTx(tx, updatePassword); err != nil {
			return err
		}
		touched, err := applyAdminUserPermissions(tx, user.Id, user.Role, operatorRole, permissions)
		authzTouched = touched
		return err
	})
	return authzTouched, err
}

// CreateAdminUserWithPermissions atomically inserts a new user and applies the
// admin permission matrix changes in the same transaction.
func CreateAdminUserWithPermissions(user *model.User, permissions map[string]map[string]bool, operatorRole int) (bool, error) {
	authzTouched := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := user.InsertWithTx(tx, 0); err != nil {
			return err
		}
		touched, err := applyAdminUserPermissions(tx, user.Id, user.Role, operatorRole, permissions)
		authzTouched = touched
		return err
	})
	return authzTouched, err
}

// DemoteUser atomically demotes a user and clears their authorization grants in
// the same transaction.
func DemoteUser(user *model.User) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := user.UpdateWithTx(tx, false); err != nil {
			return err
		}
		return authz.ClearUserAuthorizationInTx(tx, user.Id)
	})
}

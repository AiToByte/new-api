package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openUpdateSelfTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})
	return db
}

func callUpdateSelf(t *testing.T, userId int, body string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", userId)
	UpdateSelf(c)
	var output map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &output), recorder.Body.String())
	return output
}

// The self-service endpoint dispatches one route to three operations by
// payload shape. Lock the dispatch table: settings keys, profile fields,
// and the password path (which must demand step-up proof, never the profile
// update).
func TestUpdateSelfDispatchesByPayload(t *testing.T) {
	openUpdateSelfTestDB(t)
	user := model.User{Username: "self-dispatch", DisplayName: "Before", Status: 1}
	require.NoError(t, model.DB.Create(&user).Error)

	output := callUpdateSelf(t, user.Id, `{"sidebar_modules":"a,b"}`)
	assert.Equal(t, true, output["success"])
	stored, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, "a,b", stored.GetSetting().SidebarModules)

	output = callUpdateSelf(t, user.Id, `{"language":"en"}`)
	assert.Equal(t, true, output["success"])
	stored, err = model.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, "en", stored.GetSetting().Language)
	assert.Equal(t, "a,b", stored.GetSetting().SidebarModules, "language update must preserve other settings")

	output = callUpdateSelf(t, user.Id, `{"display_name":"After"}`)
	assert.Equal(t, true, output["success"])
	stored, err = model.GetUserById(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, "After", stored.DisplayName)

	output = callUpdateSelf(t, user.Id, `{"password":"new-password-123","original_password":"x"}`)
	assert.Equal(t, false, output["success"], "password path must demand step-up proof")

	// A null settings key carries no value; like an empty body it falls
	// through to the profile path and succeeds as a no-op.
	output = callUpdateSelf(t, user.Id, `{"sidebar_modules":null}`)
	assert.Equal(t, true, output["success"])
}

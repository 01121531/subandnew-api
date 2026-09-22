package controller

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReportIDsForKindPreservesLegacyRequestsAndRejectsMismatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-report.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.ManagedInstance{Name: "claude", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://claude.example.test"}).Error)
	require.NoError(t, db.Create(&model.ManagedInstance{Name: "mercer", Kind: model.ManagedInstanceKindMercerRouter, BaseURL: "https://mercer.example.test"}).Error)

	gin.SetMode(gin.TestMode)
	legacy := httptest.NewRecorder()
	legacyContext, _ := gin.CreateTestContext(legacy)
	legacyContext.Request = httptest.NewRequest("GET", "/daily-reports?instance_ids=1,2", nil)
	ids, ok := reportIDsForKind(legacyContext)
	require.True(t, ok)
	require.Equal(t, []int64{1, 2}, ids)

	mismatch := httptest.NewRecorder()
	mismatchContext, _ := gin.CreateTestContext(mismatch)
	mismatchContext.Request = httptest.NewRequest("GET", "/daily-reports?instance_ids=1&instance_kind=mercer_router", nil)
	ids, ok = reportIDsForKind(mismatchContext)
	require.False(t, ok)
	require.Nil(t, ids)
	require.Equal(t, 400, mismatch.Code)

	query := url.Values{"instance_ids": {"2"}, "instance_kind": {model.ManagedInstanceKindMercerRouter}}
	valid := httptest.NewRecorder()
	validContext, _ := gin.CreateTestContext(valid)
	validContext.Request = httptest.NewRequest("GET", "/daily-reports?"+query.Encode(), nil)
	ids, ok = reportIDsForKind(validContext)
	require.True(t, ok)
	require.Equal(t, []int64{2}, ids)
}

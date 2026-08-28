package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthMiddlewareTestRouter(t *testing.T, user *model.User) *gin.Engine {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(user).Error)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("01234567890123456789012345678901"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "stale-name")
		session.Set("role", common.RoleRootUser)
		session.Set("id", user.Id)
		session.Set("status", common.UserStatusEnabled)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/user", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"username": c.GetString("username"), "role": c.GetInt("role")})
	})
	router.GET("/admin", AdminAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return router
}

func authMiddlewareCookie(t *testing.T, router http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	cookies := response.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0]
}

func TestAuthHelperUsesCurrentDatabaseRoleAndUsername(t *testing.T) {
	user := &model.User{Username: "current-name", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	router := newAuthMiddlewareTestRouter(t, user)
	cookie := authMiddlewareCookie(t, router)

	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	request = httptest.NewRequest(http.MethodGet, "/user", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"role":1,"username":"current-name"}`, response.Body.String())
}

func TestAuthHelperRejectsDisabledCurrentUser(t *testing.T) {
	user := &model.User{Username: "disabled", Role: common.RoleRootUser, Status: common.UserStatusDisabled}
	router := newAuthMiddlewareTestRouter(t, user)
	cookie := authMiddlewareCookie(t, router)

	request := httptest.NewRequest(http.MethodGet, "/user", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}

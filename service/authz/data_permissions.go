package authz

import (
	"sync"

	"github.com/01121531/subandnew-api/common"
	"github.com/casbin/casbin/v2"
)

var dataPermissionVersions = struct {
	sync.Mutex
	enforcer *casbin.SyncedEnforcer
	users    map[int]int64
}{users: map[int]int64{}}

// A newly observed principal revision must not use another node's stale
// function-policy cache while waiting for the periodic policy synchronizer.
func (a *DataAccess) SyncPermissions() error {
	if a.Role >= common.RoleRootUser {
		return nil
	}
	dataPermissionVersions.Lock()
	defer dataPermissionVersions.Unlock()
	current := currentEnforcer()
	if dataPermissionVersions.enforcer != current {
		dataPermissionVersions.enforcer = current
		dataPermissionVersions.users = map[int]int64{}
	}
	if version, ok := dataPermissionVersions.users[a.UserID]; ok && version == a.Version {
		return nil
	}
	if err := ReloadPolicy(); err != nil {
		return ErrDataForbidden
	}
	dataPermissionVersions.users[a.UserID] = a.Version
	return nil
}

func (a *DataAccess) Can(permission Permission) bool {
	return a.SyncPermissions() == nil && Can(a.UserID, a.Role, permission)
}

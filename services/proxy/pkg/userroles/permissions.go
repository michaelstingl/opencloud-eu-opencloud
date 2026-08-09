package userroles

import (
	"sync"
	"time"

	cs3 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	settingsmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/settings/v0"
	settingssvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	"github.com/opencloud-eu/opencloud/services/settings/pkg/store/defaults"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

// CreateSpacesOpaqueKey is the key under which the role assigners record whether the user's roles
// permit creating a space. Consumers must treat an absent key as "unknown" rather than as "no":
// the assigner only sets it when it could actually resolve the role bundles.
const CreateSpacesOpaqueKey = "may-create-spaces"

// createSpacesCacheTTL is how long the role-bundle permissions are assumed to still hold, the same
// window roleNamesToRoleIDs uses for the role-name to role-id map.
const createSpacesCacheTTL = 5 * time.Minute

// createSpacesPermissionID identifies the "create spaces" permission within a role bundle. It is
// the same setting graph looks up in canCreateSpace before it lets a user create a drive.
var createSpacesPermissionID = defaults.CreateSpacesPermission(0).GetId()

// createSpacesCache holds, per role id, whether that role carries the "create spaces" permission.
// Role bundles are static in practice, so this mirrors the refresh shape of roleNameToIDCache
// rather than asking the settings service again on every request.
type createSpacesCache struct {
	byRoleID map[string]bool
	lastRead time.Time
	lock     sync.RWMutex
}

var createSpaces createSpacesCache

// applyRolesToOpaque records the user's role ids in the user's opaque data and, when it can be
// determined, whether any of those roles permits creating a space. Consumers can then answer
// "may this user have a personal space" without a lookup of their own.
//
// A failure to resolve the permission is not fatal: the roles are still recorded and the key is
// left unset, so a consumer falls back to whatever it did before.
func (o Options) applyRolesToOpaque(user *cs3.User, roleIDs []string) {
	user.Opaque = utils.AppendJSONToOpaque(user.GetOpaque(), "roles", roleIDs)

	if len(roleIDs) == 0 {
		// No role to judge by. Saying "false" here would claim knowledge we do not have.
		return
	}

	allowed, err := o.rolesAllowCreatingSpaces(roleIDs)
	if err != nil {
		o.logger.Debug().Err(err).Msg("could not determine whether the user's roles permit creating a space")
		return
	}
	user.Opaque = utils.AppendJSONToOpaque(user.GetOpaque(), CreateSpacesOpaqueKey, allowed)
}

// createSpacesByRoleIDFromBundles maps each role bundle to whether it carries the "create spaces"
// permission.
func createSpacesByRoleIDFromBundles(bundles []*settingsmsg.Bundle) map[string]bool {
	byRoleID := make(map[string]bool, len(bundles))
	for _, role := range bundles {
		for _, setting := range role.GetSettings() {
			if setting.GetId() == createSpacesPermissionID {
				byRoleID[role.GetId()] = true
				break
			}
		}
	}
	return byRoleID
}

// rolesAllowCreatingSpaces reports whether any of the given roles carries the "create spaces"
// permission, in either of its constraints: a role limited to the user's own spaces is still a
// role that may have a personal space.
func (o Options) rolesAllowCreatingSpaces(roleIDs []string) (bool, error) {
	byRoleID, err := o.createSpacesByRoleID()
	if err != nil {
		return false, err
	}

	for _, id := range roleIDs {
		if byRoleID[id] {
			return true, nil
		}
	}
	return false, nil
}

// createSpacesByRoleID returns the cached role-id to "may create spaces" map, refreshing it from
// the settings service when it has gone stale.
func (o Options) createSpacesByRoleID() (map[string]bool, error) {
	createSpaces.lock.RLock()

	if !createSpaces.lastRead.IsZero() && time.Since(createSpaces.lastRead) < createSpacesCacheTTL {
		defer createSpaces.lock.RUnlock()
		return createSpaces.byRoleID, nil
	}

	// cache needs a refresh, get a write lock
	createSpaces.lock.RUnlock()
	createSpaces.lock.Lock()
	defer createSpaces.lock.Unlock()

	// check again, another goroutine might have updated while we "upgraded" the lock
	if !createSpaces.lastRead.IsZero() && time.Since(createSpaces.lastRead) < createSpacesCacheTTL {
		return createSpaces.byRoleID, nil
	}

	// Listing roles needs elevated access to the settings service, the same way
	// roleNamesToRoleIDs does.
	ctx, err := o.prepareAdminContext()
	if err != nil {
		o.logger.Debug().Err(err).Msg("Error creating admin context")
		return nil, err
	}

	res, err := o.roleService.ListRoles(ctx, &settingssvc.ListBundlesRequest{})
	if err != nil {
		o.logger.Error().Err(err).Msg("Failed to list all roles")
		return nil, err
	}

	createSpaces.byRoleID = createSpacesByRoleIDFromBundles(res.GetBundles())
	createSpaces.lastRead = time.Now()
	return createSpaces.byRoleID, nil
}

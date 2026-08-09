package userroles

import (
	"testing"
	"time"

	cs3 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/opencloud-eu/opencloud/services/settings/pkg/store/defaults"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

// The permission is read off the role bundles the settings service actually ships, so this asserts
// against those rather than against a fixture: the guest/user-light bundle is the one role that
// carries no CreateSpaces permission, and it is also the role the default assigner hands to a guest
// who has none.
func TestCreateSpacesByRoleIDFromDefaultBundles(t *testing.T) {
	byRoleID := createSpacesByRoleIDFromBundles(defaults.GenerateBundlesDefaultRoles())

	for _, tc := range []struct {
		name   string
		roleID string
		want   bool
	}{
		{"admin", defaults.BundleUUIDRoleAdmin, true},
		{"space admin", defaults.BundleUUIDRoleSpaceAdmin, true},
		// Constrained to the user's own spaces, which is exactly what a personal space is.
		{"user", defaults.BundleUUIDRoleUser, true},
		{"user light", defaults.BundleUUIDRoleUserLight, false},
		// Same bundle as user light, under its other name.
		{"guest", defaults.BundleUUIDRoleGuest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := byRoleID[tc.roleID]; got != tc.want {
				t.Fatalf("role %s: may create spaces = %v, want %v", tc.roleID, got, tc.want)
			}
		})
	}
}

func TestApplyRolesToOpaqueRecordsThePermission(t *testing.T) {
	seedCreateSpacesCache(t)

	for _, tc := range []struct {
		name    string
		roleIDs []string
		want    bool
	}{
		{"a role that may create spaces", []string{defaults.BundleUUIDRoleUser}, true},
		{"a role that may not", []string{defaults.BundleUUIDRoleUserLight}, false},
		// A user may hold several roles; any one of them permitting a space is enough.
		{"one permitted among several", []string{defaults.BundleUUIDRoleUserLight, defaults.BundleUUIDRoleUser}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &cs3.User{Id: &cs3.UserId{OpaqueId: "u"}}
			Options{}.applyRolesToOpaque(user, tc.roleIDs)

			var got bool
			if err := utils.ReadJSONFromOpaque(user.GetOpaque(), CreateSpacesOpaqueKey, &got); err != nil {
				t.Fatalf("the key was not recorded: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%s = %v, want %v", CreateSpacesOpaqueKey, got, tc.want)
			}

			var roleIDs []string
			if err := utils.ReadJSONFromOpaque(user.GetOpaque(), "roles", &roleIDs); err != nil {
				t.Fatalf("the roles must still be recorded: %v", err)
			}
		})
	}
}

// Claiming "may not create spaces" for a user whose roles we could not resolve would suppress a
// home the user is entitled to, so the key stays unset and the consumer falls back to asking.
func TestApplyRolesToOpaqueLeavesThePermissionUnsetWhenUnknown(t *testing.T) {
	seedCreateSpacesCache(t)

	for _, tc := range []struct {
		name    string
		roleIDs []string
	}{
		{"no roles at all", nil},
		{"a role the settings service does not know", []string{"5c2b4b1e-4b0e-4b0e-8b0e-4b0e4b0e4b0e"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &cs3.User{Id: &cs3.UserId{OpaqueId: "u"}}
			Options{}.applyRolesToOpaque(user, tc.roleIDs)

			var got bool
			err := utils.ReadJSONFromOpaque(user.GetOpaque(), CreateSpacesOpaqueKey, &got)
			if tc.roleIDs == nil && err == nil {
				t.Fatal("expected the key to be absent when there is no role to judge by")
			}
			if err == nil && got {
				t.Fatal("an unknown role must not be reported as permitted")
			}
		})
	}
}

// seedCreateSpacesCache fills the package-level cache from the shipped bundles so a test does not
// need a settings service, and restores it afterwards.
func seedCreateSpacesCache(t *testing.T) {
	t.Helper()

	createSpaces.lock.Lock()
	previous, previousRead := createSpaces.byRoleID, createSpaces.lastRead
	createSpaces.byRoleID = createSpacesByRoleIDFromBundles(defaults.GenerateBundlesDefaultRoles())
	createSpaces.lastRead = time.Now()
	createSpaces.lock.Unlock()

	t.Cleanup(func() {
		createSpaces.lock.Lock()
		createSpaces.byRoleID, createSpaces.lastRead = previous, previousRead
		createSpaces.lock.Unlock()
	})
}

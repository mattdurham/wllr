package main

import (
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// builtinManifestPermissions reads built-in permission manifests from the
// embedded FS. These tests verify the manifest is the source of truth for
// built-in permissions and that loading fails closed (never an implicit
// all-permissions grant) when a manifest is missing, malformed, or declares
// an unknown permission.

func TestBuiltinManifestPermissions_Granted(t *testing.T) {
	perms := builtinManifestPermissions("agents")
	if !containsPerm(perms, sdk.PermUI) {
		t.Errorf("agents built-in should be granted ui, got %v", perms)
	}
	if containsPerm(perms, sdk.PermExec) || containsPerm(perms, sdk.PermNetworkWrite) {
		t.Errorf("agents built-in must not hold exec/network permissions, got %v", perms)
	}

	perms = builtinManifestPermissions("logging")
	if !containsPerm(perms, sdk.PermFileWrite) {
		t.Errorf("logging built-in should be granted file_write, got %v", perms)
	}
	if containsPerm(perms, sdk.PermExec) || containsPerm(perms, sdk.PermNetworkRead) {
		t.Errorf("logging built-in must not hold exec/network permissions, got %v", perms)
	}
}

func TestBuiltinManifestPermissions_None(t *testing.T) {
	for _, name := range []string{"history", "queue", "sigil"} {
		perms := builtinManifestPermissions(name)
		if len(perms) != 0 {
			t.Errorf("%s built-in should be granted no permissions, got %v", name, perms)
		}
	}
}

func TestBuiltinManifestPermissions_PlanHasUI(t *testing.T) {
	// The plan extension renders a sidebar widget and /plan command, so it
	// requires the ui permission (granted via its tracked manifest).
	perms := builtinManifestPermissions("plan")
	if len(perms) != 1 || perms[0] != sdk.PermUI {
		t.Errorf("plan built-in should be granted ui permission, got %v", perms)
	}
}

func TestBuiltinManifestPermissions_Missing_FailsClosed(t *testing.T) {
	// A built-in without a tracked manifest must get zero permissions, not an
	// implicit all-permissions grant.
	perms := builtinManifestPermissions("does-not-exist")
	if len(perms) != 0 {
		t.Errorf("missing manifest should fail closed to zero permissions, got %v", perms)
	}
}

func containsPerm(perms []sdk.Permission, p sdk.Permission) bool {
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}

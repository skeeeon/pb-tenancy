package tenancy

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// createCollections creates all required collections for tenancy.
// This is idempotent - existing collections are left unchanged.
//
// STRATEGY:
// 1. Create all collections with minimal/no rules first
// 2. Set proper API rules after all collections and fields exist
//
// COLLECTIONS CREATED:
// - organizations: Multi-tenant organization records
// - memberships: User-organization relationships with roles
// - invites: Pending organization invitations
// - users: Extended with current_organization field
//
// RETURNS:
//   - nil on successful creation
//   - error if any collection creation fails
func createCollections(app *pocketbase.PocketBase) error {
	// Step 1: Create all collections with fields (no complex rules yet)
	if err := ensureOrganizationsCollection(app); err != nil {
		return err
	}
	
	if err := ensureMembershipsCollection(app); err != nil {
		return err
	}
	
	if err := ensureInvitesCollection(app); err != nil {
		return err
	}
	
	if err := addCurrentOrgFieldToUsers(app); err != nil {
		return err
	}
	
	// Step 2: Now that all collections exist, set proper API rules
	if err := setAPIRules(app); err != nil {
		return err
	}
	
	return nil
}

// ensureOrganizationsCollection creates the organizations collection.
//
// COLLECTION SCHEMA:
// - name: Organization name (unique)
// - description: Optional description
// - owner: Foreign key to users
// - active: Enable/disable flag
//
// INDEXES:
// - Unique index on name to prevent duplicates
//
// NOTE: API rules are set later in setAPIRules() after all collections exist
func ensureOrganizationsCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("organizations")
	if err == nil {
		return nil // Already exists
	}
	
	collection := core.NewBaseCollection("organizations")
	
	// No rules yet - will be set after all collections exist
	
	// Add fields
	collection.Fields.Add(&core.TextField{
		Name:     "name",
		Required: true,
		Max:      100,
	})
	collection.Fields.Add(&core.TextField{
		Name: "description",
		Max:  500,
	})
	collection.Fields.Add(&core.BoolField{
		Name: "active",
	})
	
	// Add unique index on name
	collection.Indexes = []string{
		"CREATE UNIQUE INDEX idx_unique_org_name ON organizations (name)",
	}
	
	// Save collection first
	if err := app.Save(collection); err != nil {
		return err
	}
	
	// Add owner relation after save (needs users collection to exist)
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	
	collection.Fields.Add(&core.RelationField{
		Name:         "owner",
		Required:     true,
		MaxSelect:    1,
		CollectionId: usersCollection.Id,
	})
	
	return app.Save(collection)
}

// ensureMembershipsCollection creates the memberships collection.
//
// COLLECTION SCHEMA:
// - user: Foreign key to users
// - organization: Foreign key to organizations
// - role: User role (owner, admin, member)
// - invited_by: Foreign key to user who sent invite (null for owners)
//
// INDEXES:
// - Index on user for quick lookups
// - Index on organization for quick lookups
// - Compound index for uniqueness checks
//
// NOTE: API rules are set later in setAPIRules() after all collections exist
func ensureMembershipsCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("memberships")
	if err == nil {
		return nil
	}
	
	collection := core.NewBaseCollection("memberships")
	
	// No rules yet - will be set after all collections exist
	
	// Save collection first
	if err := app.Save(collection); err != nil {
		return err
	}
	
	// Get required collections for relations
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	
	orgsCollection, err := app.FindCollectionByNameOrId("organizations")
	if err != nil {
		return err
	}
	
	// Add fields with relations
	collection.Fields.Add(&core.RelationField{
		Name:         "user",
		Required:     true,
		MaxSelect:    1,
		CollectionId: usersCollection.Id,
	})
	collection.Fields.Add(&core.RelationField{
		Name:         "organization",
		Required:     true,
		MaxSelect:    1,
		CollectionId: orgsCollection.Id,
	})
	collection.Fields.Add(&core.SelectField{
		Name:      "role",
		Required:  true,
		MaxSelect: 1,
		Values:    []string{"owner", "admin", "member"},
	})
	collection.Fields.Add(&core.RelationField{
		Name:         "invited_by",
		MaxSelect:    1,
		CollectionId: usersCollection.Id,
	})
	
	// Add indexes for performance
	collection.Indexes = []string{
		"CREATE INDEX idx_memberships_user ON memberships (user)",
		"CREATE INDEX idx_memberships_org ON memberships (organization)",
		"CREATE INDEX idx_memberships_user_org ON memberships (user, organization)",
	}
	
	return app.Save(collection)
}

// ensureInvitesCollection creates the invites collection.
//
// COLLECTION SCHEMA:
// - email: Email address of invitee
// - organization: Foreign key to organizations
// - role: Role for new member (admin, member)
// - token: Unique secure token for acceptance
// - expires_at: When invite expires
// - invited_by: Foreign key to user who sent invite
//
// INDEXES:
// - Unique index on token for security
// - Index on email for lookups
// - Index on organization for filtering
// - Index on expires_at for cleanup queries
//
// NOTE: API rules are set later in setAPIRules() after all collections exist
func ensureInvitesCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("invites")
	if err == nil {
		return nil
	}
	
	collection := core.NewBaseCollection("invites")
	
	// No rules yet - will be set after all collections exist
	
	// Add basic fields
	collection.Fields.Add(&core.EmailField{
		Name:     "email",
		Required: true,
	})
	collection.Fields.Add(&core.TextField{
		Name:     "token",
		Required: true,
		Max:      64,
	})
	collection.Fields.Add(&core.DateField{
		Name:     "expires_at",
		Required: true,
	})
	collection.Fields.Add(&core.BoolField{
		Name: "resend_invite",
	})
	
	// Save collection first (with basic fields only)
	if err := app.Save(collection); err != nil {
		return err
	}
	
	// Get required collections for relations
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	
	orgsCollection, err := app.FindCollectionByNameOrId("organizations")
	if err != nil {
		return err
	}
	
	// Add relation fields
	collection.Fields.Add(&core.RelationField{
		Name:         "organization",
		Required:     true,
		MaxSelect:    1,
		CollectionId: orgsCollection.Id,
	})
	collection.Fields.Add(&core.SelectField{
		Name:      "role",
		Required:  true,
		MaxSelect: 1,
		Values:    []string{"admin", "member"},
	})
	collection.Fields.Add(&core.RelationField{
		Name:         "invited_by",
		Required:     true,
		MaxSelect:    1,
		CollectionId: usersCollection.Id,
	})
	
	// Now add indexes AFTER all fields exist
	collection.Indexes = []string{
		"CREATE UNIQUE INDEX idx_token ON invites (token)",
		"CREATE INDEX idx_invites_email ON invites (email)",
		"CREATE INDEX idx_invites_org ON invites (organization)",
		"CREATE INDEX idx_invites_expires ON invites (expires_at)",
	}
	
	// Final save with all fields and indexes
	return app.Save(collection)
}

// addCurrentOrgFieldToUsers adds current_organization field to users collection.
//
// FIELD SCHEMA:
// - current_organization: Optional relation to organizations
//
// NOTE: Update rule with membership validation is set later in setAPIRules()
//
// USAGE:
// Users can switch between organizations they're members of by updating this field.
func addCurrentOrgFieldToUsers(app *pocketbase.PocketBase) error {
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	
	// Check if field already exists
	if usersCollection.Fields.GetByName("current_organization") != nil {
		return nil
	}
	
	orgsCollection, err := app.FindCollectionByNameOrId("organizations")
	if err != nil {
		return err
	}
	
	usersCollection.Fields.Add(&core.RelationField{
		Name:         "current_organization",
		MaxSelect:    1,
		CollectionId: orgsCollection.Id,
	})
	
	// No rule yet - will be set after all collections exist
	
	return app.Save(usersCollection)
}

// setAPIRules sets API rules for all collections after they've been created.
// This must be called after all collections and fields exist to avoid
// reference errors.
//
// RULES SET:
// - organizations: Member-based access control
// - memberships: Self and owner access
// - invites: Owner-only access
// - users: Self-update with membership validation
//
// RETURNS:
//   - nil on success
//   - error if rule setting fails
func setAPIRules(app *pocketbase.PocketBase) error {
	// Set organizations rules
	orgsCollection, err := app.FindCollectionByNameOrId("organizations")
	if err != nil {
		return err
	}
	
	// Users can only see organizations they're members of
	// Simplified from original to avoid complexity
	orgsCollection.ListRule = types.Pointer(
		"@request.auth.id != '' && " +
		"(@collection.memberships.user.id ?= @request.auth.id && " +
		"@collection.memberships.organization.id ?= id)",
	)
	orgsCollection.ViewRule = types.Pointer(
		"@request.auth.id != '' && " +
		"(@collection.memberships.user.id ?= @request.auth.id && " +
		"@collection.memberships.organization.id ?= id)",
	)
	orgsCollection.CreateRule = types.Pointer("@request.auth.id != ''")
	orgsCollection.UpdateRule = types.Pointer("owner = @request.auth.id")
	orgsCollection.DeleteRule = types.Pointer("owner = @request.auth.id")
	
	if err := app.Save(orgsCollection); err != nil {
		return err
	}
	
	// Set memberships rules
	membershipsCollection, err := app.FindCollectionByNameOrId("memberships")
	if err != nil {
		return err
	}
	
	// Users can see their own memberships, owners can manage their org's memberships
	membershipsCollection.ListRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	membershipsCollection.ViewRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	membershipsCollection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	membershipsCollection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	membershipsCollection.DeleteRule = types.Pointer("organization.owner = @request.auth.id && user.id != @request.auth.id")
	
	if err := app.Save(membershipsCollection); err != nil {
		return err
	}
	
	// Set invites rules
	invitesCollection, err := app.FindCollectionByNameOrId("invites")
	if err != nil {
		return err
	}
	
	// Only organization owners can manage invites
	invitesCollection.ListRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.ViewRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.DeleteRule = types.Pointer("organization.owner = @request.auth.id")
	
	if err := app.Save(invitesCollection); err != nil {
		return err
	}
	
	// Set users rules
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	
	// Users can update themselves, with validation for current_organization
	usersCollection.UpdateRule = types.Pointer(
		"@request.auth.id = id && " +
		"(@request.body.current_organization = '' || " +
		"@request.body.current_organization = null || " +
		"(@collection.memberships.user.id ?= @request.auth.id && " +
		"@collection.memberships.organization.id ?= @request.body.current_organization))",
	)
	
	if err := app.Save(usersCollection); err != nil {
		return err
	}
	
	return nil
}

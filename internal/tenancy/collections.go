package tenancy

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// createCollections creates all required collections for tenancy.
// This is idempotent - existing collections are left unchanged.
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
// API RULES:
// - List/View: Members can see their organizations
// - Create: Any authenticated user
// - Update/Delete: Only organization owner
//
// INDEXES:
// - Unique index on name to prevent duplicates
func ensureOrganizationsCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("organizations")
	if err == nil {
		return nil // Already exists
	}
	
	collection := core.NewBaseCollection("organizations")
	
	// API rules - authenticated users can list/view orgs they're members of
	collection.ListRule = types.Pointer("@collection.memberships.organization.id ?= id && @collection.memberships.user.id ?= @request.auth.id && active = true")
	collection.ViewRule = types.Pointer("@collection.memberships.organization.id ?= id && @collection.memberships.user.id ?= @request.auth.id && active = true")
	collection.CreateRule = types.Pointer("@request.auth.id != ''")
	collection.UpdateRule = types.Pointer("owner = @request.auth.id")
	collection.DeleteRule = types.Pointer("owner = @request.auth.id")
	
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
	collection.Indexes = types.JsonArray[string]{
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
// API RULES:
// - List/View: Users see their own memberships, owners see all in their org
// - Create: Only organization owners
// - Update: Only organization owners
// - Delete: Only owners, but can't delete themselves
//
// INDEXES:
// - Index on user for quick lookups
// - Index on organization for quick lookups
// - Compound index for uniqueness checks
func ensureMembershipsCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("memberships")
	if err == nil {
		return nil
	}
	
	collection := core.NewBaseCollection("memberships")
	
	// API rules - users see their own, owners see all in their org
	collection.ListRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	collection.ViewRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	collection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	collection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	collection.DeleteRule = types.Pointer("organization.owner = @request.auth.id && user.id != @request.auth.id")
	
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
	collection.Indexes = types.JsonArray[string]{
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
// API RULES:
// - List/View/Create/Update/Delete: Only organization owners
//
// INDEXES:
// - Unique index on token for security
// - Index on email for lookups
// - Index on organization for filtering
// - Index on expires_at for cleanup queries
func ensureInvitesCollection(app *pocketbase.PocketBase) error {
	_, err := app.FindCollectionByNameOrId("invites")
	if err == nil {
		return nil
	}
	
	collection := core.NewBaseCollection("invites")
	
	// API rules - only organization owners can manage invites
	collection.ListRule = types.Pointer("organization.owner = @request.auth.id")
	collection.ViewRule = types.Pointer("organization.owner = @request.auth.id")
	collection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	collection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	collection.DeleteRule = types.Pointer("organization.owner = @request.auth.id")
	
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
	
	// Add indexes
	collection.Indexes = types.JsonArray[string]{
		"CREATE UNIQUE INDEX idx_token ON invites (token)",
		"CREATE INDEX idx_invites_email ON invites (email)",
		"CREATE INDEX idx_invites_org ON invites (organization)",
		"CREATE INDEX idx_invites_expires ON invites (expires_at)",
	}
	
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
	
	return app.Save(collection)
}

// addCurrentOrgFieldToUsers adds current_organization field to users collection.
// This field tracks which organization the user is currently working in.
//
// FIELD SCHEMA:
// - current_organization: Optional relation to organizations
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
	
	return app.Save(usersCollection)
}

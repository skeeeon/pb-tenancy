// FILE: internal/tenancy/collections.go

package tenancy

import (
	"fmt"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// createCollections creates all required collections for tenancy.
func createCollections(app *pocketbase.PocketBase, options Options) error {
	if err := ensureOrganizationsCollection(app, options); err != nil {
		return err
	}
	if err := ensureMembershipsCollection(app, options); err != nil {
		return err
	}
	if err := ensureInvitesCollection(app, options); err != nil {
		return err
	}
	if err := addCurrentOrgFieldToUsers(app, options); err != nil {
		return err
	}
	return nil
}

// ensureOrganizationsCollection creates the organizations collection.
func ensureOrganizationsCollection(app *pocketbase.PocketBase, options Options) error {
	name := options.OrganizationsCollection
	_, err := app.FindCollectionByNameOrId(name)
	if err == nil {
		return nil // Already exists
	}

	collection := core.NewBaseCollection(name)
	collection.Fields.Add(&core.TextField{Name: "name", Required: true, Max: 100})
	collection.Fields.Add(&core.TextField{Name: "description", Max: 500})
	collection.Fields.Add(&core.BoolField{Name: "active"})
	collection.Indexes = []string{
		fmt.Sprintf("CREATE UNIQUE INDEX idx_unique_org_name ON %s (name)", name),
	}

	if err := app.Save(collection); err != nil {
		return err
	}

	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	// IMPORTANT: CascadeDelete is intentionally false here.
	// Deleting a user should not delete the entire organization.
	// PocketBase will prevent user deletion if they are an owner, which is safer.
	collection.Fields.Add(&core.RelationField{
		Name: "owner", Required: true, MaxSelect: 1, CollectionId: usersCollection.Id,
	})
	return app.Save(collection)
}

// ensureMembershipsCollection creates the memberships collection.
func ensureMembershipsCollection(app *pocketbase.PocketBase, options Options) error {
	name := options.MembershipsCollection
	_, err := app.FindCollectionByNameOrId(name)
	if err == nil {
		return nil
	}

	collection := core.NewBaseCollection(name)
	if err := app.Save(collection); err != nil {
		return err
	}

	usersCollection, _ := app.FindCollectionByNameOrId("users")
	orgsCollection, _ := app.FindCollectionByNameOrId(options.OrganizationsCollection)

	// If a user is deleted, their membership is removed.
	collection.Fields.Add(&core.RelationField{
		Name:         "user",
		Required:     true,
		MaxSelect:    1,
		CascadeDelete: true,
		CollectionId: usersCollection.Id,
	})
	// If an organization is deleted, all its memberships are removed.
	collection.Fields.Add(&core.RelationField{
		Name:         "organization",
		Required:     true,
		MaxSelect:    1,
		CascadeDelete: true,
		CollectionId: orgsCollection.Id,
	})
	collection.Fields.Add(&core.SelectField{Name: "role", Required: true, MaxSelect: 1, Values: []string{"owner", "admin", "member"}})
	collection.Fields.Add(&core.RelationField{Name: "invited_by", MaxSelect: 1, CollectionId: usersCollection.Id})
	collection.Indexes = []string{
		fmt.Sprintf("CREATE INDEX idx_memberships_user ON %s (user)", name),
		fmt.Sprintf("CREATE INDEX idx_memberships_org ON %s (organization)", name),
		fmt.Sprintf("CREATE UNIQUE INDEX idx_memberships_user_org ON %s (user, organization)", name),
	}
	return app.Save(collection)
}

// ensureInvitesCollection creates the invites collection.
func ensureInvitesCollection(app *pocketbase.PocketBase, options Options) error {
	name := options.InvitesCollection
	_, err := app.FindCollectionByNameOrId(name)
	if err == nil {
		return nil
	}

	collection := core.NewBaseCollection(name)
	collection.Fields.Add(&core.EmailField{Name: "email", Required: true})
	collection.Fields.Add(&core.TextField{Name: "token", Required: true, Max: 64})
	collection.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
	collection.Fields.Add(&core.BoolField{Name: "resend_invite"})

	if err := app.Save(collection); err != nil {
		return err
	}

	usersCollection, _ := app.FindCollectionByNameOrId("users")
	orgsCollection, _ := app.FindCollectionByNameOrId(options.OrganizationsCollection)

	// If an organization is deleted, all its pending invites are removed.
	collection.Fields.Add(&core.RelationField{
		Name:         "organization",
		Required:     true,
		MaxSelect:    1,
		CascadeDelete: true,
		CollectionId: orgsCollection.Id,
	})
	collection.Fields.Add(&core.SelectField{Name: "role", Required: true, MaxSelect: 1, Values: []string{"admin", "member"}})
	// If the user who sent the invite is deleted, the invite is removed.
	collection.Fields.Add(&core.RelationField{
		Name:         "invited_by",
		Required:     true,
		MaxSelect:    1,
		CascadeDelete: true,
		CollectionId: usersCollection.Id,
	})
	collection.Indexes = []string{
		fmt.Sprintf("CREATE UNIQUE INDEX idx_token ON %s (token)", name),
		fmt.Sprintf("CREATE INDEX idx_invites_email ON %s (email)", name),
		fmt.Sprintf("CREATE INDEX idx_invites_org ON %s (organization)", name),
		fmt.Sprintf("CREATE INDEX idx_invites_expires ON %s (expires_at)", name),
	}
	return app.Save(collection)
}

// addCurrentOrgFieldToUsers adds current_organization field to users collection.
func addCurrentOrgFieldToUsers(app *pocketbase.PocketBase, options Options) error {
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	if usersCollection.Fields.GetByName("current_organization") != nil {
		return nil
	}
	orgsCollection, err := app.FindCollectionByNameOrId(options.OrganizationsCollection)
	if err != nil {
		return err
	}

	// When an organization is deleted, this field on the user record
	// will be automatically cleared by PocketBase.
	usersCollection.Fields.Add(&core.RelationField{
		Name: "current_organization", MaxSelect: 1, CollectionId: orgsCollection.Id,
	})
	return app.Save(usersCollection)
}

// setAPIRules sets API rules for all collections after they've been created.
func setAPIRules(app *pocketbase.PocketBase, options Options) error {
	// Organizations rules
	orgsCollection, _ := app.FindCollectionByNameOrId(options.OrganizationsCollection)
	orgsCollection.ListRule = types.Pointer(fmt.Sprintf(
		"@request.auth.id != '' && (@collection.%s.user.id ?= @request.auth.id && @collection.%s.organization.id ?= id)",
		options.MembershipsCollection, options.MembershipsCollection,
	))
	orgsCollection.ViewRule = orgsCollection.ListRule
	orgsCollection.CreateRule = types.Pointer("@request.auth.id != ''")
	orgsCollection.UpdateRule = types.Pointer("owner = @request.auth.id")
	orgsCollection.DeleteRule = types.Pointer("owner = @request.auth.id")
	if err := app.Save(orgsCollection); err != nil {
		return err
	}

	// Memberships rules
	membershipsCollection, _ := app.FindCollectionByNameOrId(options.MembershipsCollection)
	membershipsCollection.ListRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	membershipsCollection.ViewRule = types.Pointer("user.id = @request.auth.id || organization.owner = @request.auth.id")
	membershipsCollection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	membershipsCollection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	membershipsCollection.DeleteRule = types.Pointer("(organization.owner = @request.auth.id && user.id != @request.auth.id) || (user.id = @request.auth.id && role != 'owner')")
	if err := app.Save(membershipsCollection); err != nil {
		return err
	}

	// Invites rules
	invitesCollection, _ := app.FindCollectionByNameOrId(options.InvitesCollection)
	invitesCollection.ListRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.ViewRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.CreateRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.UpdateRule = types.Pointer("organization.owner = @request.auth.id")
	invitesCollection.DeleteRule = types.Pointer("organization.owner = @request.auth.id")
	if err := app.Save(invitesCollection); err != nil {
		return err
	}

	// Users rules
	usersCollection, _ := app.FindCollectionByNameOrId("users")
	usersCollection.UpdateRule = types.Pointer(fmt.Sprintf(
		"@request.auth.id = id && (@request.body.current_organization = '' || @request.body.current_organization = null || (@collection.%s.user.id ?= @request.auth.id && @collection.%s.organization.id ?= @request.body.current_organization))",
		options.MembershipsCollection, options.MembershipsCollection,
	))
	if err := app.Save(usersCollection); err != nil {
		return err
	}

	return nil
}

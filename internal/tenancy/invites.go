// FILE: internal/tenancy/invites.go

package tenancy

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerInviteEndpoint(app *pocketbase.PocketBase, options Options) error {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// UPDATED: The handler is now a closure to capture the options
		se.Router.POST("/api/tenancy/accept-invite", func(e *core.RequestEvent) error {
			return acceptInviteHandler(e, app, options)
		}).Bind(apis.RequireAuth())

		return se.Next()
	})
	return nil
}

func acceptInviteHandler(e *core.RequestEvent, app *pocketbase.PocketBase, options Options) error {
	authRecord := e.Auth
	data := struct {
		Token string `json:"token" form:"token"`
	}{}
	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Failed to read request data", err)
	}
	if data.Token == "" {
		return e.BadRequestError("Token is required", nil)
	}

	invite, err := app.FindFirstRecordByFilter(options.InvitesCollection, "token = {:token}",
		dbx.Params{"token": data.Token})
	if err != nil {
		return e.NotFoundError("Invalid or expired invitation", nil)
	}

	if time.Now().After(invite.GetDateTime("expires_at").Time()) {
		app.Delete(invite)
		return e.Error(http.StatusGone, "This invitation has expired", nil)
	}

	if authRecord.Email() != invite.GetString("email") {
		return e.ForbiddenError("This invitation is for a different user.", nil)
	}

	orgID := invite.GetString("organization")
	existingMembership, _ := app.FindFirstRecordByFilter(options.MembershipsCollection,
		"user = {:user} && organization = {:org}",
		dbx.Params{"user": authRecord.Id, "org": orgID})

	if existingMembership != nil {
		app.Delete(invite)
		return e.JSON(http.StatusOK, map[string]interface{}{
			"success": true, "alreadyMember": true, "message": "You are already a member of this organization",
		})
	}

	if err := createMembership(app, authRecord.Id, orgID, invite.GetString("role"), invite.GetString("invited_by"), options); err != nil {
		return e.InternalServerError("Failed to add membership", err)
	}

	if authRecord.GetString("current_organization") == "" {
		authRecord.Set("current_organization", orgID)
		if err := app.Save(authRecord); err != nil {
			app.Logger().Error("Failed to set current_organization after invite accept", "error", err)
		}
	}

	if err := app.Delete(invite); err != nil {
		app.Logger().Error("Failed to delete invite after acceptance", "error", err)
	}

	return e.JSON(http.StatusOK, map[string]interface{}{
		"success": true, "message": "Successfully joined organization",
	})
}

func createMembership(app *pocketbase.PocketBase, userID, orgID, role, invitedBy string, options Options) error {
	collection, err := app.FindCollectionByNameOrId(options.MembershipsCollection)
	if err != nil {
		return err
	}
	membership := core.NewRecord(collection)
	membership.Set("user", userID)
	membership.Set("organization", orgID)
	membership.Set("role", role)
	if invitedBy != "" {
		membership.Set("invited_by", invitedBy)
	}
	return app.Save(membership)
}

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

	// Find the invite record
	invite, err := app.FindFirstRecordByFilter(options.InvitesCollection, "token = {:token}",
		dbx.Params{"token": data.Token})
	if err != nil {
		return e.NotFoundError("Invalid or expired invitation", err)
	}

	// --- Pre-transaction checks and early exits ---

	// Check if expired
	if time.Now().After(invite.GetDateTime("expires_at").Time()) {
		app.Delete(invite) // Cleanup expired invite
		return e.Error(http.StatusGone, "This invitation has expired", nil)
	}

	// Check if the invite is for the authenticated user
	if authRecord.Email() != invite.GetString("email") {
		return e.ForbiddenError("This invitation is for a different user.", nil)
	}

	orgID := invite.GetString("organization")

	// Check if user is already a member
	existingMembership, _ := app.FindFirstRecordByFilter(options.MembershipsCollection,
		"user = {:user} && organization = {:org}",
		dbx.Params{"user": authRecord.Id, "org": orgID})

	if existingMembership != nil {
		app.Delete(invite) // Cleanup the now-redundant invite
		return e.JSON(http.StatusOK, map[string]interface{}{
			"message":       "You are already a member of this organization.",
			"alreadyMember": true,
		})
	}

	// --- Transactional block for creating membership and cleaning up ---
	// Use app.RunInTransaction to ensure atomicity.
	err = app.RunInTransaction(func(txApp core.App) error {
		// 1. Create the membership record
		if err := createMembership(txApp, authRecord.Id, orgID, invite.GetString("role"), invite.GetString("invited_by"), options); err != nil {
			return err
		}

		// 2. If user has no active org, set this one as current
		// It's safer to fetch the record again inside the transaction.
		userToUpdate, err := txApp.FindRecordById("users", authRecord.Id)
		if err != nil {
			return err
		}
		if userToUpdate.GetString("current_organization") == "" {
			userToUpdate.Set("current_organization", orgID)
			if err := txApp.Save(userToUpdate); err != nil {
				return err
			}
		}

		// 3. Delete the invite record
		if err := txApp.Delete(invite); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return e.InternalServerError("Failed to accept invitation", err)
	}

	return e.JSON(http.StatusOK, map[string]interface{}{
		"message": "Successfully joined organization.",
	})
}

// createMembership creates a new membership record using a core.App instance,
// making it compatible with transactions.
func createMembership(app core.App, userID, orgID, role, invitedBy string, options Options) error {
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

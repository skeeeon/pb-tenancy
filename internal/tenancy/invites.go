package tenancy

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// registerInviteEndpoint registers the /api/tenancy/accept-invite endpoint.
//
// ENDPOINT:
// POST /api/tenancy/accept-invite
//
// BEHAVIOR:
// - Requires an authenticated user session.
// - Validates invite token, expiry, and ensures the invite is for the authenticated user.
// - Adds the user to the organization as a member.
// - Deletes the invite after successful acceptance.
//
// RATE LIMITING:
// Automatically applied via PocketBase's built-in rate limiter
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - options: Configuration options
//
// RETURNS:
//   - nil on successful registration
//   - error if endpoint registration fails
func registerInviteEndpoint(app *pocketbase.PocketBase, options Options) error {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// CORRECTED: The middleware is chained with .Bind()
		se.Router.POST("/api/tenancy/accept-invite", func(e *core.RequestEvent) error {
			return acceptInviteHandler(e, app)
		}).Bind(apis.RequireAuth())

		return se.Next()
	})
	return nil
}

// acceptInviteHandler handles the accept-invite endpoint for authenticated users.
//
// REQUEST BODY:
// - token: Invite token (required)
//
// RESPONSES:
// - 200: Invitation accepted successfully.
// - 400: Invalid request data (e.g., missing token).
// - 401: User is not authenticated.
// - 403: Invitation is for a different user.
// - 404: Invalid or expired invitation.
// - 410: Invitation has expired.
//
// PARAMETERS:
//   - e: Request event
//   - app: PocketBase application instance
//
// RETURNS:
//   - nil on success (response sent)
//   - error for PocketBase to handle
func acceptInviteHandler(e *core.RequestEvent, app *pocketbase.PocketBase) error {
	// CORRECTED: Access the authenticated user via the e.Auth field
	authRecord := e.Auth

	// Parse request body
	data := struct {
		Token string `json:"token" form:"token"`
	}{}

	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Failed to read request data", err)
	}

	if data.Token == "" {
		return e.BadRequestError("Token is required", nil)
	}

	// Find invite by token
	invite, err := app.FindFirstRecordByFilter("invites", "token = {:token}",
		dbx.Params{"token": data.Token})
	if err != nil {
		return e.NotFoundError("Invalid or expired invitation", nil)
	}

	// Check expiry
	if time.Now().After(invite.GetDateTime("expires_at").Time()) {
		// Clean up the expired invite
		app.Delete(invite)
		return e.Error(http.StatusGone, "This invitation has expired", nil)
	}

	// SECURITY CHECK: Ensure the authenticated user is the one being invited.
	if authRecord.Email() != invite.GetString("email") {
		return e.ForbiddenError("This invitation is for a different user.", nil)
	}

	// Check if user is already a member of the target organization
	orgID := invite.GetString("organization")
	existingMembership, _ := app.FindFirstRecordByFilter("memberships",
		"user = {:user} && organization = {:org}",
		dbx.Params{
			"user": authRecord.Id,
			"org":  orgID,
		})

	if existingMembership != nil {
		// Already a member - the invitation is now redundant.
		// Delete the invite and inform the user.
		app.Delete(invite)
		return e.JSON(http.StatusOK, map[string]interface{}{
			"success":       true,
			"alreadyMember": true,
			"message":       "You are already a member of this organization",
		})
	}

	// Create the new membership record
	if err := createMembership(app, authRecord.Id,
		orgID,
		invite.GetString("role"),
		invite.GetString("invited_by")); err != nil {
		return e.InternalServerError("Failed to add membership", err)
	}

	// Set as current organization if the user doesn't have one set
	if authRecord.GetString("current_organization") == "" {
		authRecord.Set("current_organization", orgID)
		if err := app.Save(authRecord); err != nil {
			// Log the error but don't fail the request, as the core action (joining) succeeded.
			app.Logger().Error("Failed to set current_organization after invite accept", "error", err)
		}
	}

	// Delete the invite now that it has been successfully used
	if err := app.Delete(invite); err != nil {
		// Log the error but don't fail the request, as the user has already joined.
		app.Logger().Error("Failed to delete invite after acceptance", "error", err)
	}

	return e.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Successfully joined organization",
	})
}

// createMembership creates a membership record linking user to organization.
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - userID: User record ID
//   - orgID: Organization record ID
//   - role: User role (admin, member)
//   - invitedBy: ID of user who sent invite
//
// RETURNS:
//   - nil on successful membership creation
//   - error if creation fails
func createMembership(app *pocketbase.PocketBase, userID, orgID, role, invitedBy string) error {
	collection, err := app.FindCollectionByNameOrId("memberships")
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

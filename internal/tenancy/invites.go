package tenancy

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// registerInviteEndpoint registers the /api/tenancy/accept-invite endpoint.
//
// ENDPOINT:
// POST /api/tenancy/accept-invite
//
// BEHAVIOR:
// - Validates invite token and expiry
// - Creates new user account if email doesn't exist
// - Adds user to organization as member
// - Deletes invite after successful acceptance
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
		se.Router.POST("/api/tenancy/accept-invite", func(e *core.RequestEvent) error {
			return acceptInviteHandler(e, app)
		})
		return se.Next()
	})
	return nil
}

// acceptInviteHandler handles the accept-invite endpoint.
//
// REQUEST BODY:
// - token: Invite token (required)
// - password: Password for new user account (required for new users)
// - passwordConfirm: Password confirmation (required for new users)
//
// RESPONSES:
// - 200: Invitation accepted successfully (existing user joined)
// - 201: Invitation accepted successfully (new user created)
// - 400: Invalid request data
// - 404: Invalid or expired invitation
// - 410: Invitation has expired
//
// PARAMETERS:
//   - e: Request event
//   - app: PocketBase application instance
//
// RETURNS:
//   - nil on success (response sent)
//   - error for PocketBase to handle
func acceptInviteHandler(e *core.RequestEvent, app *pocketbase.PocketBase) error {
	// Parse request body
	data := struct {
		Token           string `json:"token" form:"token"`
		Password        string `json:"password" form:"password"`
		PasswordConfirm string `json:"passwordConfirm" form:"passwordConfirm"`
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
		// Delete expired invite
		app.Delete(invite)
		return e.Error(http.StatusGone, "This invitation has expired", nil)
	}
	
	// Check if user exists
	email := invite.GetString("email")
	existingUser, _ := app.FindFirstRecordByFilter("users", "email = {:email}",
		dbx.Params{"email": email})
	
	if existingUser == nil {
		// NEW USER PATH: Create account and add to organization
		return handleNewUserInvite(e, app, invite, data.Password, data.PasswordConfirm)
	} else {
		// EXISTING USER PATH: Just add to organization
		return handleExistingUserInvite(e, app, invite, existingUser)
	}
}

// handleNewUserInvite creates a new user account and adds them to the organization.
//
// BEHAVIOR:
// - Validates password requirements
// - Creates verified user account
// - Sets current_organization
// - Creates membership record
// - Deletes invite
//
// PARAMETERS:
//   - e: Request event
//   - app: PocketBase application instance
//   - invite: Invite record
//   - password: User's chosen password
//   - passwordConfirm: Password confirmation
//
// RETURNS:
//   - nil on success (201 response sent)
//   - error for PocketBase to handle
func handleNewUserInvite(e *core.RequestEvent, app *pocketbase.PocketBase, 
	invite *core.Record, password, passwordConfirm string) error {
	
	if password == "" || password != passwordConfirm {
		return e.BadRequestError("Valid password is required", nil)
	}
	
	// Create user account
	usersCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return e.InternalServerError("Failed to find users collection", err)
	}
	
	userRecord := core.NewRecord(usersCollection)
	userRecord.Set("email", invite.GetString("email"))
	userRecord.Set("password", password)
	userRecord.Set("passwordConfirm", passwordConfirm)
	userRecord.Set("verified", true)
	userRecord.Set("current_organization", invite.GetString("organization"))
	
	if err := app.Save(userRecord); err != nil {
		return e.BadRequestError("Failed to create account", err)
	}
	
	// Create membership
	if err := createMembership(app, userRecord.Id, 
		invite.GetString("organization"), 
		invite.GetString("role"),
		invite.GetString("invited_by")); err != nil {
		return e.InternalServerError("Account created but failed to add membership", err)
	}
	
	// Delete invite
	app.Delete(invite)
	
	return e.JSON(http.StatusCreated, map[string]interface{}{
		"success": true,
		"message": "Account created successfully",
	})
}

// handleExistingUserInvite adds an existing user to the organization.
//
// BEHAVIOR:
// - Checks if already a member
// - Creates membership record
// - Sets current_organization if not already set
// - Deletes invite
//
// PARAMETERS:
//   - e: Request event
//   - app: PocketBase application instance
//   - invite: Invite record
//   - userRecord: Existing user record
//
// RETURNS:
//   - nil on success (200 response sent)
//   - error for PocketBase to handle
func handleExistingUserInvite(e *core.RequestEvent, app *pocketbase.PocketBase,
	invite *core.Record, userRecord *core.Record) error {
	
	// Check if already member
	existing, _ := app.FindFirstRecordByFilter("memberships",
		"user = {:user} && organization = {:org}",
		dbx.Params{
			"user": userRecord.Id,
			"org":  invite.GetString("organization"),
		})
	
	if existing != nil {
		// Already a member - delete invite and return success
		app.Delete(invite)
		return e.JSON(http.StatusOK, map[string]interface{}{
			"success":       true,
			"alreadyMember": true,
			"message":       "You are already a member of this organization",
		})
	}
	
	// Create membership
	if err := createMembership(app, userRecord.Id,
		invite.GetString("organization"),
		invite.GetString("role"),
		invite.GetString("invited_by")); err != nil {
		return e.InternalServerError("Failed to add membership", err)
	}
	
	// Set as current org if user doesn't have one
	if userRecord.GetString("current_organization") == "" {
		userRecord.Set("current_organization", invite.GetString("organization"))
		app.Save(userRecord)
	}
	
	// Delete invite
	app.Delete(invite)
	
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

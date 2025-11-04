package tenancy

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

// registerHooks sets up automatic behaviors for tenancy.
//
// HOOKS REGISTERED:
// 1. Organization created → auto-create owner membership
// 2. Invite created → auto-set token, expiry, invited_by
// 3. Invite created → send email
// 4. Invite updated with resend_invite=true → send email again
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - options: Configuration options
//
// RETURNS:
//   - nil on successful hook registration
//   - error if registration fails
func registerHooks(app *pocketbase.PocketBase, options Options) error {
	// Hook 1: Organization created → auto-create owner membership
	app.OnRecordAfterCreateSuccess("organizations").BindFunc(func(e *core.RecordEvent) error {
		return autoCreateOwnerMembership(app, e.Record)
	})
	
	// Hook 2: Invite created → auto-set token and expiry
	app.OnRecordCreateRequest("invites").BindFunc(func(e *core.RecordRequestEvent) error {
		return autoSetInviteFields(app, e.Record, e, options)
	})
	
	// Hook 3: Invite created → send email
	app.OnRecordAfterCreateSuccess("invites").BindFunc(func(e *core.RecordEvent) error {
		if err := sendInviteEmail(app, e.Record, options); err != nil {
			// Log error but don't fail - email issues shouldn't block invite creation
			if options.LogToConsole {
				fmt.Printf("⚠️  WARNING Failed to send invite email: %v\n", err)
			}
		}
		return nil
	})
	
	// Hook 4: Invite updated with resend_invite → send email again
	app.OnRecordUpdateRequest("invites").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Record.GetBool("resend_invite") {
			// Clear flag to prevent loops
			e.Record.Set("resend_invite", false)
		}
		return e.Next()
	})
	
	app.OnRecordAfterUpdateSuccess("invites").BindFunc(func(e *core.RecordEvent) error {
		// Check if this was a resend request (flag was true before clearing)
		if e.Record.OriginalCopy().GetBool("resend_invite") {
			if err := sendInviteEmail(app, e.Record, options); err != nil {
				if options.LogToConsole {
					fmt.Printf("⚠️  WARNING Failed to resend invite email: %v\n", err)
				}
			}
		}
		return nil
	})
	
	return nil
}

// autoCreateOwnerMembership creates owner membership when organization is created.
//
// BEHAVIOR:
// - Creates membership record with role="owner"
// - Sets organization as user's current_organization
// - Ensures organization owner is automatically a member
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - orgRecord: Newly created organization record
//
// RETURNS:
//   - nil on successful membership creation
//   - error if membership creation fails
func autoCreateOwnerMembership(app *pocketbase.PocketBase, orgRecord *core.Record) error {
	ownerID := orgRecord.GetString("owner")
	
	collection, err := app.FindCollectionByNameOrId("memberships")
	if err != nil {
		return err
	}
	
	membership := core.NewRecord(collection)
	membership.Set("user", ownerID)
	membership.Set("organization", orgRecord.Id)
	membership.Set("role", "owner")
	
	if err := app.Save(membership); err != nil {
		return err
	}
	
	// Set as current organization for the owner
	userRecord, err := app.FindRecordById("users", ownerID)
	if err == nil {
		userRecord.Set("current_organization", orgRecord.Id)
		app.Save(userRecord)
	}
	
	return nil
}

// autoSetInviteFields automatically sets token, expiry, and invited_by fields.
//
// BEHAVIOR:
// - Generates secure random token
// - Sets expiry date based on configuration
// - Sets invited_by to current authenticated user
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - record: Invite record being created
//   - e: Request event for accessing auth context
//   - options: Configuration options
//
// RETURNS:
//   - nil on successful field setting
//   - error if token generation fails
func autoSetInviteFields(app *pocketbase.PocketBase, record *core.Record, e *core.RecordRequestEvent, options Options) error {
	// Generate secure token
	token, err := generateSecureToken()
	if err != nil {
		return err
	}
	record.Set("token", token)
	
	// Set expiration
	expiresAt := time.Now().AddDate(0, 0, options.InviteExpiryDays)
	record.Set("expires_at", expiresAt)
	
	// Set invited_by to current authenticated user
	if e.Request != nil {
		info, err := e.RequestInfo()
		if err == nil && info.Auth != nil {
			record.Set("invited_by", info.Auth.Id)
		}
	}
	
	return nil
}

// generateSecureToken generates a cryptographically secure random token.
//
// TOKEN PROPERTIES:
// - 32 bytes of random data
// - Base64 URL-encoded for safe transmission
// - Suitable for use in URLs and form submissions
//
// RETURNS:
//   - Secure token string
//   - error if random generation fails
func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Email template for invitations.
// Uses Go text/template for safe HTML generation.
const inviteEmailTemplate = `<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px;">
    <h2>You've been invited to join {{.OrgName}}</h2>
    
    <p>{{.InviterName}} has invited you to join {{.OrgName}} as a {{.Role}}.</p>
    
    <div style="margin: 30px 0;">
        <a href="{{.InviteLink}}" 
           style="background-color: #4CAF50; color: white; padding: 12px 24px; 
                  text-decoration: none; border-radius: 4px; display: inline-block;">
            Accept Invitation
        </a>
    </div>
    
    <p style="color: #999; font-size: 12px;">
        This invitation expires on {{.ExpiresAt}}.<br>
        If you didn't expect this invitation, you can safely ignore this email.
    </p>
</body>
</html>`

// sendInviteEmail sends invitation email to the invitee.
//
// BEHAVIOR:
// - Expands organization and invited_by relations
// - Generates email from template
// - Sends using PocketBase mailer
// - Failures are logged but don't block invite creation
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - inviteRecord: Invite record to send email for
//   - options: Configuration options
//
// RETURNS:
//   - nil on successful email send
//   - error if template execution or sending fails
func sendInviteEmail(app *pocketbase.PocketBase, inviteRecord *core.Record, options Options) error {
	// Expand relations
	if err := app.ExpandRecord(inviteRecord, []string{"organization", "invited_by"}, nil); err != nil {
		return fmt.Errorf("failed to expand invite relations: %w", err)
	}
	
	org := inviteRecord.ExpandedOne("organization")
	inviter := inviteRecord.ExpandedOne("invited_by")
	
	if org == nil || inviter == nil {
		return fmt.Errorf("failed to get expanded relations")
	}
	
	// Parse template
	tmpl, err := template.New("invite").Parse(inviteEmailTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}
	
	// Prepare email data
	appName := options.AppName
	if appName == "" {
		appName = app.Settings().Meta.AppName
	}
	
	appURL := options.AppURL
	if appURL == "" {
		appURL = app.Settings().Meta.AppURL
	}
	
	data := map[string]string{
		"OrgName":     org.GetString("name"),
		"InviterName": getDisplayName(inviter),
		"Role":        inviteRecord.GetString("role"),
		"InviteLink":  fmt.Sprintf("%s/accept-invite?token=%s", appURL, inviteRecord.GetString("token")),
		"ExpiresAt":   inviteRecord.GetDateTime("expires_at").Time().Format("January 2, 2006"),
	}
	
	// Execute template
	var body strings.Builder
	if err := tmpl.Execute(&body, data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}
	
	// Send using PocketBase mailer
	message := &mailer.Message{
		From: mail.Address{
			Name:    appName,
			Address: app.Settings().Meta.SenderAddress,
		},
		To:      []mail.Address{{Address: inviteRecord.GetString("email")}},
		Subject: fmt.Sprintf("You've been invited to join %s", org.GetString("name")),
		HTML:    body.String(),
	}
	
	return app.NewMailClient().Send(message)
}

// getDisplayName returns a display name for a user record.
// Tries name field first, falls back to email.
func getDisplayName(record *core.Record) string {
	if name := record.GetString("name"); name != "" {
		return name
	}
	return record.GetString("email")
}

// FILE: internal/tenancy/tenancy.go

// Package tenancy implements multi-organization tenancy for PocketBase.
package tenancy

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase"
)

// Options holds configuration for tenancy setup.
type Options struct {
	InviteExpiryDays        int
	AppName                 string
	AppURL                  string
	LogToConsole            bool
	OrganizationsCollection string
	MembershipsCollection   string
	InvitesCollection       string
}

// Initialize sets up all tenancy components in the correct order.
func Initialize(app *pocketbase.PocketBase, options Options) error {
	if options.LogToConsole {
		log.Println("🚀 START Initializing PocketBase multi-tenancy...")
	}

	// UPDATED: Check if all tenancy collections exist.
	_, errOrgs := app.FindCollectionByNameOrId(options.OrganizationsCollection)
	_, errMems := app.FindCollectionByNameOrId(options.MembershipsCollection)
	_, errInvs := app.FindCollectionByNameOrId(options.InvitesCollection)
	isFirstTimeSetup := errOrgs != nil || errMems != nil || errInvs != nil

	if isFirstTimeSetup {
		if options.LogToConsole {
			log.Println("ℹ️  INFO   Performing first-time setup for tenancy collections...")
		}

		// Step 1: Create collections.
		if err := createCollections(app, options); err != nil {
			return fmt.Errorf("failed to create collections: %w", err)
		}
		if options.LogToConsole {
			log.Println("✅ SUCCESS Collections created")
		}

		// Step 2: Set API rules since this is the first time.
		if err := setAPIRules(app, options); err != nil {
			return fmt.Errorf("failed to set API rules: %w", err)
		}
		if options.LogToConsole {
			log.Println("✅ SUCCESS Default API rules applied")
		}
	} else {
		if options.LogToConsole {
			log.Println("ℹ️  INFO   Tenancy collections already exist. Skipping schema modifications.")
		}
	}

	// Step 3: Register hooks for automatic behaviors (always do this)
	if err := registerHooks(app, options); err != nil {
		return fmt.Errorf("failed to register hooks: %w", err)
	}
	if options.LogToConsole {
		log.Println("✅ SUCCESS Hooks registered")
	}

	// Step 4: Register accept-invite endpoint (always do this)
	if err := registerInviteEndpoint(app, options); err != nil {
		return fmt.Errorf("failed to register invite endpoint: %w", err)
	}
	if options.LogToConsole {
		log.Println("✅ SUCCESS Accept-invite endpoint registered")
	}

	if options.LogToConsole {
		log.Println("✅ SUCCESS PocketBase multi-tenancy initialized successfully")
		log.Printf("ℹ️  INFO   - Organizations collection: %s", options.OrganizationsCollection)
		log.Printf("ℹ️  INFO   - Memberships collection: %s", options.MembershipsCollection)
		log.Printf("ℹ️  INFO   - Invites collection: %s", options.InvitesCollection)
		log.Printf("ℹ️  INFO   - Invite expiry: %d days", options.InviteExpiryDays)
	}

	return nil
}

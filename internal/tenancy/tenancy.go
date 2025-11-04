// Package tenancy implements multi-organization tenancy for PocketBase.
package tenancy

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase"
)

// Options holds configuration for tenancy setup.
type Options struct {
	InviteExpiryDays int
	AppName          string
	AppURL           string
	LogToConsole     bool
}

// Initialize sets up all tenancy components in the correct order.
//
// INITIALIZATION SEQUENCE:
// 1. Collections - Create required database structures
// 2. Hooks - Register automatic behaviors
// 3. Endpoints - Register API routes
//
// PARAMETERS:
//   - app: PocketBase application instance
//   - options: Configuration options
//
// RETURNS:
//   - nil on successful initialization
//   - error if any component fails
func Initialize(app *pocketbase.PocketBase, options Options) error {
	if options.LogToConsole {
		log.Println("🚀 START Initializing PocketBase multi-tenancy...")
	}
	
	// Step 1: Create collections (idempotent)
	if err := createCollections(app); err != nil {
		return fmt.Errorf("failed to create collections: %w", err)
	}
	
	if options.LogToConsole {
		log.Println("✅ SUCCESS Collections initialized")
	}
	
	// Step 2: Register hooks for automatic behaviors
	if err := registerHooks(app, options); err != nil {
		return fmt.Errorf("failed to register hooks: %w", err)
	}
	
	if options.LogToConsole {
		log.Println("✅ SUCCESS Hooks registered")
	}
	
	// Step 3: Register accept-invite endpoint
	if err := registerInviteEndpoint(app, options); err != nil {
		return fmt.Errorf("failed to register invite endpoint: %w", err)
	}
	
	if options.LogToConsole {
		log.Println("✅ SUCCESS Accept-invite endpoint registered")
	}
	
	if options.LogToConsole {
		log.Println("✅ SUCCESS PocketBase multi-tenancy initialized successfully")
		log.Printf("ℹ️  INFO   - Organizations collection: organizations")
		log.Printf("ℹ️  INFO   - Memberships collection: memberships")
		log.Printf("ℹ️  INFO   - Invites collection: invites")
		log.Printf("ℹ️  INFO   - Invite expiry: %d days", options.InviteExpiryDays)
	}
	
	return nil
}

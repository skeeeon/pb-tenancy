// FILE: tenancy.go

// Package pbtenancy provides multi-organization tenancy for PocketBase applications.
package pbtenancy

import (
	"fmt"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/skeeeon/pb-tenancy/internal/tenancy"
)

// Options configures the behavior of multi-tenancy.
type Options struct {
	// Invite configuration
	InviteExpiryDays int    // Days until invite expires (default: 7)
	AppName          string // App name for emails (default: from settings)
	AppURL           string // App URL for invite links (default: from settings)

	// Logging
	LogToConsole bool // Enable logging (default: true)

	// NEW: Customizable collection names
	OrganizationsCollection string // Name for the organizations collection (default: "organizations")
	MembershipsCollection   string // Name for the memberships collection (default: "memberships")
	InvitesCollection       string // Name for the invites collection (default: "invites")
}

// DefaultOptions returns sensible defaults for tenancy configuration.
func DefaultOptions() Options {
	return Options{
		InviteExpiryDays:        7,
		LogToConsole:            true,
		OrganizationsCollection: "organizations",
		MembershipsCollection:   "memberships",
		InvitesCollection:       "invites",
	}
}

// Setup initializes multi-tenancy for a PocketBase application.
// This is the main entry point that creates collections and registers hooks.
func Setup(app *pocketbase.PocketBase, options Options) error {
	// Apply defaults for any zero-value fields
	options = applyDefaults(options)

	// Validate options
	if err := validateOptions(options); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	// Convert public Options to internal Options
	internalOpts := tenancy.Options{
		InviteExpiryDays:        options.InviteExpiryDays,
		AppName:                 options.AppName,
		AppURL:                  options.AppURL,
		LogToConsole:            options.LogToConsole,
		OrganizationsCollection: options.OrganizationsCollection,
		MembershipsCollection:   options.MembershipsCollection,
		InvitesCollection:       options.InvitesCollection,
	}

	// Initialize after app bootstrap
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		return tenancy.Initialize(app, internalOpts)
	})

	return nil
}

// applyDefaults fills in default values for missing options.
func applyDefaults(options Options) Options {
	defaults := DefaultOptions()

	if options.InviteExpiryDays == 0 {
		options.InviteExpiryDays = defaults.InviteExpiryDays
	}
	if options.OrganizationsCollection == "" {
		options.OrganizationsCollection = defaults.OrganizationsCollection
	}
	if options.MembershipsCollection == "" {
		options.MembershipsCollection = defaults.MembershipsCollection
	}
	if options.InvitesCollection == "" {
		options.InvitesCollection = defaults.InvitesCollection
	}

	return options
}

// validateOptions validates the provided options.
func validateOptions(options Options) error {
	if options.InviteExpiryDays < 1 {
		return fmt.Errorf("invite expiry must be at least 1 day, got: %d", options.InviteExpiryDays)
	}
	if options.OrganizationsCollection == "" || options.MembershipsCollection == "" || options.InvitesCollection == "" {
		return fmt.Errorf("collection names cannot be empty")
	}
	return nil
}

// Version returns the library version.
const Version = "1.0.0"

// GetVersion returns the library version.
func GetVersion() string {
	return Version
}

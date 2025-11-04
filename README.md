# PocketBase Multi-Tenancy Library (pb-tenancy)

A minimal library that adds multi-organization tenancy to [PocketBase](https://pocketbase.io/) applications.

## Features

- ✅ **User belongs to multiple organizations** with roles (owner/admin/member)
- ✅ **Secure invitation system** with email via PocketBase mailer
- ✅ **Organization context** (`current_organization` field tracks active org)
- ✅ **Automatic owner membership** on organization creation
- ✅ **Non-destructive setup** - Preserves your custom API rule changes after initial setup
- ✅ **Configurable collection names** - Avoid conflicts with your existing schema
- ✅ **Simple, explicit behavior** - Easy to understand and debug
- ✅ **Self-contained** - Bootstraps its own collections and hooks

## Philosophy

- **One entry point** - `Setup()` does everything
- **Uses PocketBase built-ins** (email, hooks, collections, API rules)
- **Minimal abstraction** - Clear functions that do one thing
- **Self-contained** - Bootstraps its own collections and hooks
- **Preserves customizations** - Won't overwrite your API rules after the initial setup
- **No background cleanup** - Developers handle when needed

## Installation

```bash
go get github.com/skeeeon/pb-tenancy
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/pocketbase/pocketbase"
    "github.com/skeeeon/pb-tenancy"
)

func main() {
    app := pocketbase.New()
    
    // Setup tenancy with defaults
    if err := pbtenancy.Setup(app, pbtenancy.DefaultOptions()); err != nil {
        log.Fatal(err)
    }
    
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

## Data Model

The library creates the following collections. You can customize the names via the `Options`.

#### `organizations` (default name)
| Field | Type | Description |
|-------|------|-------------|
| `name` | text (unique) | Organization name |
| `description` | text | Optional description |
| `owner` | relation → users | Organization owner |
| `active` | bool | Enable/disable flag |

**API Rules:**
- List/View: Members can see their organizations
- Create: Any authenticated user
- Update/Delete: Only organization owner

#### `memberships` (default name)
| Field | Type | Description |
|-------|------|-------------|
| `user` | relation → users | Member user (unique with organization) |
| `organization` | relation → organizations | Organization |
| `role` | select | owner, admin, member |
| `invited_by` | relation → users | Who sent invite (null for owners) |

**API Rules:**
- List/View: Users see their own, owners see all in their org
- Create/Update: Only organization owners
- Delete: Owners can remove others (but not themselves); members can remove themselves to leave.

#### `invites` (default name)
| Field | Type | Description |
|-------|------|-------------|
| `email` | email | Invitee email |
| `organization` | relation → organizations | Target organization |
| `role` | select | admin, member |
| `token` | text (unique) | Secure acceptance token |
| `expires_at` | date | Expiration date |
| `invited_by` | relation → users | Who sent invite |
| `resend_invite` | bool | Trigger to resend email |

**API Rules:**
- All operations: Only organization owners

#### `users` (Modified)
**Added field:**
| Field | Type | Description |
|-------|------|-------------|
| `current_organization` | relation → organizations | Current active organization |

## Configuration

```go
options := pbtenancy.DefaultOptions()

// Customize invite expiry (default: 7 days)
options.InviteExpiryDays = 14

// Custom app name for emails (default: from PocketBase settings)
options.AppName = "My IoT Platform"

// Custom app URL for invite links (default: from PocketBase settings)
options.AppURL = "https://myapp.com"

// Customize collection names to avoid conflicts
options.OrganizationsCollection = "companies"
options.MembershipsCollection = "company_members"
options.InvitesCollection = "company_invites"

// Disable console logging (default: true)
options.LogToConsole = false

pbtenancy.Setup(app, options)
```

## Usage Examples

### Creating an Organization

```javascript
// Client-side (JavaScript SDK)
const org = await pb.collection('organizations').create({
    name: 'Acme Corp',
    description: 'My company',
    owner: pb.authStore.model.id
})
// Hook automatically creates owner membership!
```

### Sending an Invitation

```javascript
await pb.collection('invites').create({
    email: 'teammate@example.com',
    organization: org.id,
    role: 'member'
})
// Hook automatically:
// - Generates secure token, sets expiry date, and sets invited_by
// - Sends an email to the user!
```

### Resending an Invitation

```javascript
await pb.collection('invites').update(inviteId, {
    resend_invite: true
})
// Email sent again with same token/expiry
```

### Accepting an Invitation

Accepting an invitation is a secure, two-step process. The user **must be authenticated** to accept.

**Onboarding Flow:**
1.  A user receives an invite email and clicks the link.
2.  Your application directs them to a page (e.g., `/accept-invite?token=...`).
3.  If the user is not logged in, your UI should prompt them to **log in or sign up**. If they are a new user, they must create an account with the same email address the invitation was sent to.
4.  Once authenticated, your application can make the API call to accept the invite.

```javascript
// 1. User must be authenticated first
await pb.collection('users').authWithPassword('user@example.com', 'password');

// 2. Make the API call with the token
await fetch('/api/tenancy/accept-invite', {
    method: 'POST',
    headers: {
        'Content-Type': 'application/json',
        'Authorization': pb.authStore.token // Pass the auth token
    },
    body: JSON.stringify({
        token: tokenFromURL
    })
});
// On success, the user is added to the organization and the invite is deleted.
```

### Switching Organizations

```javascript
// Just update the user's current_organization field!
await pb.collection('users').update(pb.authStore.model.id, {
    current_organization: newOrgId
})
```

### Listing a User's Organizations

```javascript
const memberships = await pb.collection('memberships').getFullList({
    filter: `user = "${pb.authStore.model.id}"`,
    expand: 'organization'
});

memberships.forEach(membership => {
    console.log(membership.expand.organization.name, membership.role);
});
```

### Leaving an Organization

```javascript
// Members can delete their own membership record to leave.
// Owners can remove other members, but cannot remove themselves.
await pb.collection('memberships').delete(membershipId);
```

## What Developers Need to Handle

We don't do everything for you:

### 1. Manual Cleanup of Expired Invites

```javascript
// Run this periodically (cron job, etc.)
const now = new Date().toISOString();
const expired = await pb.collection('invites').getFullList({
    filter: `expires_at < "${now}"`
});

for (const invite of expired) {
    await pb.collection('invites').delete(invite.id);
}
```

### 2. Organization-Scoped Queries

You are responsible for scoping your own collection queries to the user's active organization.

```javascript
// Query data for the current organization
const devices = await pb.collection('devices').getFullList({
    filter: `organization = "${pb.authStore.model.current_organization}"`
});
```

### 3. Advanced Member Visibility

If you want members to see other members in their organization, update the API rules for the `memberships` collection:

```
// In memberships collection API rules:
// list: "user.id = @request.auth.id || 
//        organization.owner = @request.auth.id || 
//        organization.id = @request.auth.current_organization"
```

## Testing Checklist

- ✅ Organization creation auto-creates owner membership
- ✅ Invite creation generates token and sends email
- ✅ Accept invite adds membership for authenticated user
- ✅ Expired invites are rejected with 410 Gone
- ✅ Accepting an invite for an organization you're already in handles gracefully
- ✅ Owner cannot delete own membership (API rule)
- ✅ Member can leave organization by deleting their membership (API rule)
- ✅ Only owners can manage invites (API rule)
- ✅ Unique organization names are enforced
- ✅ Resend invite works without modifying token/expiry

## Troubleshooting

### Emails Not Sending

Check PocketBase email settings in the Admin UI → Settings → Mail settings, or via environment variables. Email failures are logged but do not block invite creation.

### Invite Token Not Working

- Check if the user is authenticated before trying to accept.
- Check if the token has expired (`expires_at < now`).
- Check if the invite was already accepted (it is deleted after acceptance).

## Contributing

Contributions welcome! Please adhere to the project's philosophy of simple, explicit, and debuggable code.

## License

MIT License.

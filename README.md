# PocketBase Multi-Tenancy Library (pb-tenancy)

A minimal library that adds multi-organization tenancy to [PocketBase](https://pocketbase.io/) applications.

## Features

- ✅ **User belongs to multiple organizations** with roles (owner/admin/member)
- ✅ **Invitation system** with email via PocketBase mailer
- ✅ **Organization context** (`current_organization` field tracks active org)
- ✅ **Automatic owner membership** on organization creation
- ✅ **Secure invite tokens** with expiration
- ✅ **Simple, explicit behavior** - easy to understand and debug
- ✅ **Self-contained** - bootstraps its own collections and hooks

## Philosophy

- **One entry point** - `Setup()` does everything
- **Uses PocketBase built-ins** (email, hooks, collections, API rules)
- **Minimal abstraction** - clear functions that do one thing
- **Self-contained** - bootstraps its own collections and hooks
- **No background cleanup** - developers handle when needed
- **No helper functions** - developers know their patterns

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
    
    app.Start()
}
```

## Data Model

### Collections Schema

#### `organizations`
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

#### `memberships`
| Field | Type | Description |
|-------|------|-------------|
| `user` | relation → users | Member user |
| `organization` | relation → organizations | Organization |
| `role` | select | owner, admin, member |
| `invited_by` | relation → users | Who sent invite (null for owners) |

**API Rules:**
- List/View: Users see their own, owners see all in their org
- Create/Update: Only organization owners
- Delete: Only owners, but can't delete themselves

#### `invites`
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
// - Generates secure token
// - Sets expiry date
// - Sets invited_by to current user
// - Sends email!
```

### Resending an Invitation

```javascript
await pb.collection('invites').update(inviteId, {
    resend_invite: true
})
// Email sent again with same token/expiry
```

### Accepting an Invitation (New User)

```javascript
await fetch('/api/tenancy/accept-invite', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
        token: tokenFromURL,
        password: 'secure-password',
        passwordConfirm: 'secure-password'
    })
})
// Creates user account, adds to organization, deletes invite
```

### Accepting an Invitation (Existing User)

```javascript
// User must be authenticated first
await pb.collection('users').authWithPassword('user@example.com', 'password')

await fetch('/api/tenancy/accept-invite', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
        token: tokenFromURL
    })
})
// Adds user to organization, deletes invite
```

### Switching Organizations

```javascript
// Just update the field!
await pb.collection('users').update(pb.authStore.model.id, {
    current_organization: newOrgId
})
```

### Listing User's Organizations

```javascript
const memberships = await pb.collection('memberships').getFullList({
    filter: `user = "${pb.authStore.model.id}"`,
    expand: 'organization'
})

memberships.forEach(membership => {
    console.log(membership.expand.organization.name, membership.role)
})
```

### Leaving an Organization

```javascript
// Just delete the membership (API rules prevent deleting yourself if you're owner)
await pb.collection('memberships').delete(membershipId)
```

## Integration with Other Libraries

### With pb-nats (NATS JWT Authentication)

```go
func main() {
    app := pocketbase.New()
    
    // Setup tenancy first
    pbtenancy.Setup(app, pbtenancy.DefaultOptions())
    
    // Setup NATS integration
    pbnats.Setup(app, pbnats.DefaultOptions())
    
    app.Start()
}
```

### With pb-audit (Audit Logging)

```go
func main() {
    app := pocketbase.New()
    
    // Setup tenancy
    pbtenancy.Setup(app, pbtenancy.DefaultOptions())
    
    // Setup audit logging (logs all tenancy operations)
    pbaudit.Setup(app, pbaudit.DefaultOptions())
    
    app.Start()
}
```

## What Developers Need to Handle

We don't do everything for you:

### 1. Manual Cleanup of Expired Invites

```javascript
// Run this periodically (cron job, etc.)
const now = new Date().toISOString()
const expired = await pb.collection('invites').getFullList({
    filter: `expires_at < "${now}"`
})

for (const invite of expired) {
    await pb.collection('invites').delete(invite.id)
}
```

### 2. Custom Notification Channels

```javascript
// After creating invite, send via your preferred channel
const invite = await pb.collection('invites').create({...})

if (userType === 'technician') {
    await sendSMS(user.phone, inviteLink)
} else {
    await sendSlack(user.slackId, inviteLink)
}
```

### 3. Organization-Scoped Queries

```javascript
// Query data for current organization
const devices = await pb.collection('devices').getFullList({
    filter: `organization = "${pb.authStore.model.current_organization}"`
})
```

### 4. Advanced Member Visibility

If you want members to see other members in their organization, update the API rules:

```javascript
// In memberships collection API rules:
// list: "user.id = @request.auth.id || 
//        organization.owner = @request.auth.id || 
//        organization.id = @request.auth.current_organization"
```

## API Rules Customization

The library sets up sensible defaults, but you can customize:

### Example: Role-Based Visibility

```javascript
// memberships collection
list: "user.id = @request.auth.id || 
       organization.owner = @request.auth.id ||
       (organization.id = @request.auth.current_organization && 
        @collection.memberships.role ?= 'admin')"
```

### Example: Organization-Scoped Collections

When creating your own collections:

```javascript
// Add organization field
collection.Fields.Add(&core.RelationField{
    Name:         "organization",
    Required:     true,
    CollectionId: organizationsCollection.Id,
})

// Scope list rule to current organization
collection.ListRule = types.Pointer(
    "organization = @request.auth.current_organization"
)
```

## Testing Checklist

- ✅ Organization creation auto-creates owner membership
- ✅ Invite creation generates token and sends email
- ✅ Accept invite creates user + membership (new user)
- ✅ Accept invite adds membership (existing user)
- ✅ Expired invites rejected with 410 Gone
- ✅ Already-member handles gracefully
- ✅ Owner cannot delete own membership (API rule)
- ✅ Only owners can manage invites (API rule)
- ✅ Unique organization names enforced
- ✅ Resend invite works without modifying token/expiry

## IoT Platform Integration Example

```go
func main() {
    app := pocketbase.New()
    
    // Setup tenancy
    pbtenancy.Setup(app, pbtenancy.DefaultOptions())
    
    // Setup NATS
    pbnats.Setup(app, pbnats.DefaultOptions())
    
    // Create IoT collections with organization scoping
    app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
        createEdgesCollection(app)
        createThingsCollection(app)
        return e.Next()
    })
    
    app.Start()
}

func createEdgesCollection(app *pocketbase.PocketBase) {
    collection := core.NewBaseCollection("edges")
    
    // Organization scoping
    orgsCollection, _ := app.FindCollectionByNameOrId("organizations")
    collection.Fields.Add(&core.RelationField{
        Name:         "organization",
        Required:     true,
        CollectionId: orgsCollection.Id,
    })
    
    // API rules - org scoped
    collection.ListRule = types.Pointer(
        "organization = @request.auth.current_organization"
    )
    collection.CreateRule = types.Pointer(
        "@request.auth.id != '' && organization = @request.auth.current_organization"
    )
    
    // Other fields...
    
    app.Save(collection)
}
```

## Troubleshooting

### Emails Not Sending

Check PocketBase email settings:
```bash
# Admin UI → Settings → Mail settings
# Or via environment variables
```

Email failures are logged but don't block invite creation.

### Invite Token Not Working

- Check if token expired (`expires_at < now`)
- Check if token is correct (case-sensitive, URL-encoded)
- Check if invite was already accepted (deleted after acceptance)

### User Can't See Organization

Check:
- User has membership in that organization
- Organization is active (`active = true`)
- Membership query includes proper filter

### Organization Name Already Exists

Organization names must be unique. Either:
- Choose different name
- Delete existing organization with that name
- Update existing organization instead

## Contributing

Contributions welcome! 
- Simple, explicit code
- Clear function purposes
- Minimal abstractions
- Easy to debug

## License

MIT License - see LICENSE file for details.

## Related Libraries

- [pb-nats](https://github.com/skeeeon/pb-nats) - NATS JWT authentication
- [pb-audit](https://github.com/skeeeon/pb-audit) - Audit logging

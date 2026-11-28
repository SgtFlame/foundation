// Package providers holds the OAuth providers Foundation knows (forge
// 26a-2, documentation/Connectors.md › Built in forge 26a-2): Google,
// Microsoft and Slack. Each is metadata only; connector types name one in
// Type.OAuth, or let a connection choose one (Type.OAuth.Field).
package providers

import (
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The providers' ids.
const (
	Google    = "google"
	Microsoft = "microsoft"
	Slack     = "slack"
)

// Scopes the mail, calendar and chat types ask for.
const (
	GoogleMail          = "https://mail.google.com/"
	GoogleCalendar      = "https://www.googleapis.com/auth/calendar"
	MicrosoftIMAP       = "https://outlook.office.com/IMAP.AccessAsUser.All"
	MicrosoftPOP        = "https://outlook.office.com/POP.AccessAsUser.All"
	MicrosoftSMTP       = "https://outlook.office.com/SMTP.Send"
	MicrosoftCalendars  = "https://graph.microsoft.com/Calendars.ReadWrite"
	MicrosoftChannelMsg = "https://graph.microsoft.com/ChannelMessage.Send"
)

// GoogleProvider is Google's: offline access with consent every time, so
// each sign-in gives a refresh token; the account is the id_token's email.
func GoogleProvider() connectors.OAuthProvider {
	return connectors.OAuthProvider{
		ID: Google, Title: "Google",
		AuthorizeURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:      "https://oauth2.googleapis.com/token",
		RevokeURL:     "https://oauth2.googleapis.com/revoke",
		PKCE:          true,
		Extra:         map[string]string{"access_type": "offline", "prompt": "consent", "include_granted_scopes": "true"},
		DefaultScopes: []string{"openid", "email"},
		Scopes: map[string]string{
			"openid":       "Signs in with OpenID Connect (the id_token)",
			"email":        "Reads the account's email address",
			"profile":      "Reads the account's name and picture",
			GoogleMail:     "Reads, sends and deletes the account's Gmail over IMAP, POP and SMTP",
			GoogleCalendar: "Reads and changes every calendar the account can reach",
			"https://www.googleapis.com/auth/calendar.events":   "Reads and changes events on the account's calendars",
			"https://www.googleapis.com/auth/calendar.readonly": "Reads the account's calendars",
		},
		AccountField: "id_token.email",
		Register:     "https://console.cloud.google.com/apis/credentials",
	}
}

// MicrosoftProvider is the Microsoft identity platform (v2.0): the
// directory comes from the registration (common when empty). One token
// is for one resource, so a choice asks for outlook.office.com scopes
// (IMAP, POP, SMTP) or graph.microsoft.com ones, never both.
func MicrosoftProvider() connectors.OAuthProvider {
	return connectors.OAuthProvider{
		ID: Microsoft, Title: "Microsoft",
		AuthorizeURL:  "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/authorize",
		TokenURL:      "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token",
		TenantSegment: true,
		PKCE:          true,
		DefaultScopes: []string{"openid", "profile", "offline_access"},
		Scopes: map[string]string{
			"openid":                                "Signs in with OpenID Connect (the id_token)",
			"profile":                               "Reads the account's name and user name",
			"email":                                 "Reads the account's email address",
			"offline_access":                        "Keeps access while the person is away (a refresh token)",
			MicrosoftIMAP:                           "Reads and changes the mailbox over IMAP",
			MicrosoftPOP:                            "Reads the mailbox over POP3",
			MicrosoftSMTP:                           "Sends mail as the account over SMTP",
			MicrosoftCalendars:                      "Reads and changes the account's calendars through Microsoft Graph",
			"https://graph.microsoft.com/User.Read": "Reads the account's profile through Microsoft Graph",
			"https://graph.microsoft.com/Mail.ReadWrite":          "Reads and changes the mailbox through Microsoft Graph",
			"https://graph.microsoft.com/Chat.ReadWrite":          "Reads and sends Teams chat messages",
			MicrosoftChannelMsg:                                   "Sends messages to Teams channels",
			"https://graph.microsoft.com/ChannelMessage.Read.All": "Reads messages in Teams channels",
		},
		AccountField: "id_token.preferred_username",
		Register:     "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
	}
}

// SlackProvider is Slack's OAuth v2 (a bot token for the workspace): no
// PKCE for confidential apps, scopes joined by commas, the account the
// workspace and the person who installed it.
func SlackProvider() connectors.OAuthProvider {
	return connectors.OAuthProvider{
		ID: Slack, Title: "Slack",
		AuthorizeURL:   "https://slack.com/oauth/v2/authorize",
		TokenURL:       "https://slack.com/api/oauth.v2.access",
		RevokeURL:      "https://slack.com/api/auth.revoke",
		ScopeSeparator: ",",
		Scopes: map[string]string{
			"channels:read":    "Lists the workspace's public channels",
			"channels:history": "Reads messages in public channels the app is in",
			"groups:read":      "Lists private channels the app is in",
			"groups:history":   "Reads messages in private channels the app is in",
			"im:read":          "Lists direct messages with the app",
			"im:history":       "Reads direct messages with the app",
			"chat:write":       "Posts, edits and deletes the app's messages",
			"reactions:read":   "Reads reactions",
			"reactions:write":  "Adds and removes the app's reactions",
			"files:read":       "Reads files shared where the app is",
			"files:write":      "Uploads files as the app",
			"users:read":       "Reads the workspace's members",
			"users:read.email": "Reads members' email addresses",
			"team:read":        "Reads the workspace's name and domain",
		},
		AccountField: "{team.id}/{authed_user.id}",
		Register:     "https://api.slack.com/apps",
	}
}

// All are every provider here.
func All() []connectors.OAuthProvider {
	return []connectors.OAuthProvider{GoogleProvider(), MicrosoftProvider(), SlackProvider()}
}

// Contribute registers them.
func Contribute(reg *extensions.Registry) {
	for _, p := range All() {
		connectors.ContributeOAuthProvider(reg, "connectors", p)
	}
}

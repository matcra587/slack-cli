package slackcatalog

import "strings"

// ApplyPolicy is used by the generator. Reads are an explicit allowlist reviewed
// against each method's purpose, never inferred from GET or a name prefix.
// Unknown operations remain mutations until reviewed.
func ApplyPolicy(method *Method) {
	method.ReadOnly = readOnly[method.Name]
	method.Authentication = "bearer"
	method.ResponseFormat = "json"
	if method.Name == "api.test" {
		method.Authentication = "none"
	}
	if strings.HasPrefix(method.Name, "oauth.") || strings.HasPrefix(method.Name, "openid.connect.") {
		method.Authentication = "oauth"
		method.UnsupportedReason = "OAuth and OpenID endpoints require their own authentication flow"
	}
	if strings.HasPrefix(method.Name, "apps.manifest.") || strings.HasPrefix(method.Name, "apps.permissions.") || strings.HasPrefix(method.Name, "apps.token.") {
		method.Authentication = "configuration"
		method.UnsupportedReason = "requires a configuration token; generic calls support workspace bearer authentication only"
	}
	if method.Name == "apps.connections.open" {
		method.Authentication = "app"
		method.UnsupportedReason = "requires an app-level token; generic calls support workspace bearer authentication only"
	}
	if method.Name == "admin.analytics.getFile" {
		method.ResponseFormat = "binary"
		method.UnsupportedReason = "returns a compressed file instead of a JSON response"
	}
	if method.Name == "apps.icon.set" || method.Name == "users.setPhoto" {
		method.UnsupportedReason = "requires a multipart file upload; use the typed SDK operation"
	}
	method.Deprecated = deprecated[method.Name]
	if method.Deprecated {
		method.UnsupportedReason = "Slack documents this endpoint as deprecated or retired"
	}
	method.SensitiveResponse = sensitive[method.Name] || strings.HasPrefix(method.Name, "oauth.") || strings.HasPrefix(method.Name, "openid.connect.")
	if strings.HasPrefix(method.Name, "search.") && method.Pagination == "cursor" {
		method.CursorInitial = "*"
	}
	if method.Source == "sdk" {
		method.ReadOnly = false
		method.UnsupportedReason = "SDK endpoint is absent from Slack's current method index; its request contract needs review"
	}
}

var sensitive = map[string]bool{
	"apps.auth.external.get":     true,
	"apps.auth.external.delete":  true,
	"apps.connections.open":      true,
	"apps.manifest.create":       true,
	"apps.manifest.export":       true,
	"files.getUploadURLExternal": true,
	"files.sharedPublicURL":      true,
	"slackLists.download.get":    true,
	"apps.token.rotate":          true,
	"rtm.connect":                true,
	"rtm.start":                  true,
	"tooling.tokens.rotate":      true,
}

var deprecated = map[string]bool{
	"files.upload":    true,
	"rtm.start":       true,
	"stars.add":       true,
	"stars.list":      true,
	"stars.remove":    true,
	"users.setActive": true,
}

var readOnly = map[string]bool{
	"admin.analytics.getFile":                                  true,
	"admin.apps.activities.list":                               true,
	"admin.apps.approved.list":                                 true,
	"admin.apps.config.lookup":                                 true,
	"admin.apps.mcp.servers.list":                              true,
	"admin.apps.mcp.servers.permissions.list":                  true,
	"admin.apps.permissions.list":                              true,
	"admin.apps.requests.list":                                 true,
	"admin.apps.restricted.list":                               true,
	"admin.audit.anomaly.allow.getItem":                        true,
	"admin.auth.policy.getEntities":                            true,
	"admin.barriers.list":                                      true,
	"admin.conversations.ekm.listOriginalConnectedChannelInfo": true,
	"admin.conversations.getConversationPrefs":                 true,
	"admin.conversations.getCustomRetention":                   true,
	"admin.conversations.getTeams":                             true,
	"admin.conversations.lookup":                               true,
	"admin.conversations.restrictAccess.listGroups":            true,
	"admin.conversations.search":                               true,
	"admin.emoji.list":                                         true,
	"admin.functions.list":                                     true,
	"admin.functions.permissions.lookup":                       true,
	"admin.inviteRequests.approved.list":                       true,
	"admin.inviteRequests.denied.list":                         true,
	"admin.inviteRequests.list":                                true,
	"admin.roles.listAssignments":                              true,
	"admin.slackbot.skills.list":                               true,
	"admin.teams.admins.list":                                  true,
	"admin.teams.list":                                         true,
	"admin.teams.owners.list":                                  true,
	"admin.teams.settings.info":                                true,
	"admin.usergroups.listChannels":                            true,
	"admin.users.getExpiration":                                true,
	"admin.users.list":                                         true,
	"admin.users.session.getSettings":                          true,
	"admin.users.session.list":                                 true,
	"admin.workflows.permissions.lookup":                       true,
	"admin.workflows.search":                                   true,
	"admin.workflows.triggers.types.permissions.lookup":        true,
	"agents.conversations.getCanvas":                           true,
	"agents.conversations.listViews":                           true,
	"api.test":                                                 true,
	"apps.activities.list":                                     true,
	"apps.datastore.bulkGet":                                   true,
	"apps.datastore.get":                                       true,
	"apps.datastore.query":                                     true,
	"apps.event.authorizations.list":                           true,
	"apps.manifest.export":                                     true,
	"assistant.search.context":                                 true,
	"assistant.search.info":                                    true,
	"auth.teams.list":                                          true,
	"auth.test":                                                true,
	"bookmarks.list":                                           true,
	"bots.info":                                                true,
	"calls.info":                                               true,
	"canvases.getContent":                                      true,
	"canvases.sections.lookup":                                 true,
	"chat.getPermalink":                                        true,
	"chat.scheduledMessages.list":                              true,
	"conversations.history":                                    true,
	"conversations.info":                                       true,
	"conversations.list":                                       true,
	"conversations.listConnectInvites":                         true,
	"conversations.members":                                    true,
	"conversations.replies":                                    true,
	"conversations.requestSharedInvite.list":                   true,
	"dnd.info":                                                 true,
	"dnd.teamInfo":                                             true,
	"emoji.list":                                               true,
	"files.info":                                               true,
	"files.list":                                               true,
	"files.remote.info":                                        true,
	"files.remote.list":                                        true,
	"functions.distributions.permissions.list":                 true,
	"functions.workflows.steps.list":                           true,
	"pins.list":                                                true,
	"reactions.get":                                            true,
	"reactions.list":                                           true,
	"reminders.info":                                           true,
	"reminders.list":                                           true,
	"search.all":                                               true,
	"search.files":                                             true,
	"search.messages":                                          true,
	"slackLists.download.get":                                  true,
	"slackLists.items.info":                                    true,
	"slackLists.items.list":                                    true,
	"slackbot.skills.info":                                     true,
	"slackbot.skills.list":                                     true,
	"stars.list":                                               true,
	"team.accessLogs":                                          true,
	"team.billableInfo":                                        true,
	"team.billing.info":                                        true,
	"team.externalTeams.list":                                  true,
	"team.info":                                                true,
	"team.preferences.list":                                    true,
	"team.profile.get":                                         true,
	"usergroups.list":                                          true,
	"usergroups.users.list":                                    true,
	"users.conversations":                                      true,
	"users.discoverableContacts.lookup":                        true,
	"users.getPresence":                                        true,
	"users.identity":                                           true,
	"users.info":                                               true,
	"users.list":                                               true,
	"users.lookupByEmail":                                      true,
	"users.profile.get":                                        true,
	"workflows.featured.list":                                  true,
	"workflows.triggers.permissions.list":                      true,
}

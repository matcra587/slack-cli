// Package cache implements the `slick cache` cobra command tree, which primes,
// prints, and clears the local Slack metadata caches.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gechr/clog"
	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	cliuser "github.com/matcra587/slack-cli/internal/cli/user"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

const (
	// metadataTTLMinutes is the freshness window for cached Slack metadata.
	metadataTTLMinutes = 24 * 60
	defaultPageSize    = 200
	defaultMaxPages    = 20
)

// Resources lists the cacheable resource names.
var Resources = []string{"users", "channels"}

// usersPayload is the on-disk payload for the cached active-user list.
type usersPayload struct {
	Users     []clioutput.CliUser `json:"users"`
	Truncated bool                `json:"truncated,omitempty"`
}

// channelsPayload is the on-disk payload for the cached conversation list.
type channelsPayload struct {
	Channels  []clioutput.CliChannel `json:"channels"`
	Truncated bool                   `json:"truncated,omitempty"`
}

// LoadCachedUsers returns the cached user list for the profile, or
// (nil, false) if the cache is missing, stale, or fails to decode.
func LoadCachedUsers(profile string) ([]clioutput.CliUser, bool) {
	var payload usersPayload
	if !readCache(profile, "users", &payload) {
		return nil, false
	}
	return payload.Users, true
}

// LoadCachedChannels returns the cached channel list for the profile, or
// (nil, false) if the cache is missing, stale, or fails to decode.
func LoadCachedChannels(profile string) ([]clioutput.CliChannel, bool) {
	var payload channelsPayload
	if !readCache(profile, "channels", &payload) {
		return nil, false
	}
	return payload.Channels, true
}

func readCache(profile, resource string, target any) bool {
	entry, ok, stale, err := slackcache.Read(profile, resource, time.Duration(metadataTTLMinutes)*time.Minute)
	if err != nil || !ok || stale {
		return false
	}
	return json.Unmarshal(entry.Data, target) == nil
}

// UsersData is the result returned by `slick cache users`.
type UsersData struct {
	Profile   string              `json:"profile"`
	Users     []clioutput.CliUser `json:"users"`
	Count     int                 `json:"count"`
	FromCache bool                `json:"from_cache"`
	FetchedAt time.Time           `json:"fetched_at"`
	Truncated bool                `json:"truncated,omitempty"`
}

// ChannelsData is the result returned by `slick cache channels`.
type ChannelsData struct {
	Profile   string                 `json:"profile"`
	Channels  []clioutput.CliChannel `json:"channels"`
	Count     int                    `json:"count"`
	FromCache bool                   `json:"from_cache"`
	FetchedAt time.Time              `json:"fetched_at"`
	Truncated bool                   `json:"truncated,omitempty"`
}

// ClearData is the result returned by `slick cache clear`.
type ClearData struct {
	Profile      string   `json:"profile"`
	Resource     string   `json:"resource,omitempty"`
	Removed      bool     `json:"removed,omitempty"`
	RemovedCount int      `json:"removed_count,omitempty"`
	Resources    []string `json:"resources,omitempty"`
}

var _ clioutput.PlainRenderer = UsersData{}

func (d UsersData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	writeCacheSummary(c, command, d.Profile, "users", d.Count, d.FromCache, d.Truncated, d.FetchedAt)
	return nil
}

var _ clioutput.PlainRenderer = ChannelsData{}

func (d ChannelsData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	writeCacheSummary(c, command, d.Profile, "channels", d.Count, d.FromCache, d.Truncated, d.FetchedAt)
	return nil
}

func writeCacheSummary(c *clioutput.CommandContext, command, profile, resource string, count int, fromCache, truncated bool, fetchedAt time.Time) {
	logger := c.StdoutLogger()
	clioutput.ApplyFieldStyles(logger, c.Theme,
		clioutput.HashedFieldStyle("profile", "workspace:"+profile),
	)
	if truncated {
		clioutput.ApplyBoolStateStyle(logger, c.Theme, "truncated", true)
	}
	event := c.ResultEvent(command).
		Str("profile", profile).
		Str("resource", resource).
		Int("count", count).
		Bool("from_cache", fromCache).
		When(truncated, func(e *clog.Event) { e.Bool("truncated", true) })
	// Only show fetched_at when serving from cache; for a fresh fetch
	// from_cache=false already says "just now".
	if fromCache && !fetchedAt.IsZero() {
		event = event.Time("fetched_at", fetchedAt)
	}
	event.Msg(clioutput.ActionLabel(command))
}

var _ clioutput.PlainRenderer = ClearData{}

func (d ClearData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	logger := c.StdoutLogger()
	clioutput.ApplyFieldStyles(logger, c.Theme,
		clioutput.HashedFieldStyle("profile", "workspace:"+d.Profile),
	)
	// "cache clear <resource>" — single explicit resource.
	if d.Resource != "" {
		event := c.ResultEvent(command).Str("profile", d.Profile).Str("resource", d.Resource).Bool("removed", d.Removed)
		if d.Removed {
			event.Msg(clioutput.ActionLabel(command))
			return nil
		}
		event.Msg("Cache already empty")
		return nil
	}
	// "cache clear" with no args — sweep the profile.
	if d.RemovedCount == 0 {
		c.ResultEvent(command).Str("profile", d.Profile).Msg("Cache already empty")
		return nil
	}
	event := c.ResultEvent(command).Str("profile", d.Profile)
	if len(d.Resources) > 0 {
		event = event.Str("resources", strings.Join(d.Resources, ","))
	}
	event.Int("removed_count", d.RemovedCount).Msg(clioutput.ActionLabel(command))
	return nil
}

// Options collects the cache CLI flags.
type Options struct {
	Refresh    bool
	TTLMinutes int
	PageSize   int
	MaxPages   int
}

// TTL returns the configured freshness window as a duration.
func (o Options) TTL() time.Duration {
	if o.TTLMinutes <= 0 {
		return 0
	}
	return time.Duration(o.TTLMinutes) * time.Minute
}

// NewCommand returns the `slick cache` parent command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Prime and inspect local Slack metadata caches",
	}
	cmd.AddCommand(newUsersCommand(runtime))
	cmd.AddCommand(newChannelsCommand(runtime))
	cmd.AddCommand(newClearCommand(runtime))
	return cmd
}

func newUsersCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:          "users",
		Short:        "Cache and print active Slack users",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsers(cmd, runtime, opts)
		},
	}
	addFlags(cmd, &opts)
	return cmd
}

func newChannelsCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:          "channels",
		Short:        "Cache and print active Slack conversations",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChannels(cmd, runtime, opts)
		},
	}
	addFlags(cmd, &opts)
	return cmd
}

func newClearCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "clear [resource]",
		Short:        "Clear local Slack metadata caches",
		Args:         cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs:    Resources,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClear(cmd, runtime, args)
		},
	}
	return cmd
}

func defaultOptions() Options {
	return Options{
		TTLMinutes: metadataTTLMinutes,
		PageSize:   defaultPageSize,
		MaxPages:   defaultMaxPages,
	}
}

func addFlags(cmd *cobra.Command, opts *Options) {
	cmd.Flags().BoolVarP(&opts.Refresh, "refresh", "r", false, "Force a fetch even when the cache is fresh")
	cmd.Flags().IntVarP(&opts.TTLMinutes, "ttl-minutes", "T", metadataTTLMinutes, "Freshness window before automatic refresh")
	cmd.Flags().IntVarP(&opts.PageSize, "page-size", "s", defaultPageSize, "Slack page size while priming")
	cmd.Flags().IntVarP(&opts.MaxPages, "max-pages", "N", defaultMaxPages, "Maximum Slack pages to fetch")
}

func runUsers(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts Options) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	data, fromCache, fetchedAt, err := readOrFetch(profile.Name, "users", opts.TTL(), opts.Refresh, func() (json.RawMessage, error) {
		return fetchUsers(cmd, runtime, profile, opts)
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliErrorFromCacheError(cmd.Context(), err))
	}
	var payload usersPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(fmt.Sprintf("cache.users: decode cached payload: %v", err)))
	}
	return ctx.WriteResult("cache.users", UsersData{
		Profile:   profile.Name,
		Users:     payload.Users,
		Count:     len(payload.Users),
		FromCache: fromCache,
		FetchedAt: fetchedAt.UTC(),
		Truncated: payload.Truncated,
	})
}

func runChannels(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts Options) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	data, fromCache, fetchedAt, err := readOrFetch(profile.Name, "channels", opts.TTL(), opts.Refresh, func() (json.RawMessage, error) {
		return fetchChannels(cmd, runtime, profile, opts)
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliErrorFromCacheError(cmd.Context(), err))
	}
	var payload channelsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(fmt.Sprintf("cache.channels: decode cached payload: %v", err)))
	}
	return ctx.WriteResult("cache.channels", ChannelsData{
		Profile:   profile.Name,
		Channels:  payload.Channels,
		Count:     len(payload.Channels),
		FromCache: fromCache,
		FetchedAt: fetchedAt.UTC(),
		Truncated: payload.Truncated,
	})
}

func runClear(cmd *cobra.Command, runtime *cliruntime.RootRuntime, args []string) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if len(args) == 0 {
		resources, err := slackcache.ClearProfile(profile.Name)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		return ctx.WriteResult("cache.clear", ClearData{Profile: profile.Name, Resources: resources, RemovedCount: len(resources)})
	}
	resource := args[0]
	removed, err := slackcache.Clear(profile.Name, resource)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	return ctx.WriteResult("cache.clear", ClearData{Profile: profile.Name, Resource: resource, Removed: removed})
}

func readOrFetch(profile, resource string, ttl time.Duration, refresh bool, fetch func() (json.RawMessage, error)) (json.RawMessage, bool, time.Time, error) {
	if !refresh {
		if entry, ok, stale, err := slackcache.Read(profile, resource, ttl); err == nil && ok && !stale {
			return entry.Data, true, entry.FetchedAt, nil
		}
	}
	data, err := fetch()
	if err != nil {
		return nil, false, time.Time{}, err
	}
	entry, err := slackcache.Write(profile, resource, data)
	if err != nil {
		return nil, false, time.Time{}, err
	}
	return entry.Data, false, entry.FetchedAt, nil
}

func fetchUsers(cmd *cobra.Command, runtime *cliruntime.RootRuntime, profile config.WorkspaceProfile, opts Options) (json.RawMessage, error) {
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return nil, fetchError{err: clioutput.AuthCLIError(err.Error())}
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("users:read")); err != nil {
		return nil, fetchError{err: clioutput.CliErrorFromSlack(cmd.Context(), err)}
	}
	users, truncated, err := drainUsers(cmd.Context(), client, opts)
	if err != nil {
		return nil, err
	}
	return marshalPayload(usersPayload{Users: users, Truncated: truncated})
}

func fetchChannels(cmd *cobra.Command, runtime *cliruntime.RootRuntime, profile config.WorkspaceProfile, opts Options) (json.RawMessage, error) {
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return nil, fetchError{err: clioutput.AuthCLIError(err.Error())}
	}
	types := []string{"public_channel", "private_channel", "im", "mpim"}
	if err := cliscope.Require(cmd.Context(), client, clichannel.ConversationReadScopeRequirement(types)); err != nil {
		return nil, fetchError{err: clioutput.CliErrorFromSlack(cmd.Context(), err)}
	}
	channels, truncated, err := drainChannels(cmd.Context(), client, opts, types)
	if err != nil {
		return nil, err
	}
	return marshalPayload(channelsPayload{Channels: channels, Truncated: truncated})
}

func drainUsers(ctx context.Context, client *slackgo.Client, opts Options) ([]clioutput.CliUser, bool, error) {
	pager := client.GetUsersPaginated(slackgo.GetUsersOptionLimit(normalizePageSize(opts.PageSize)))
	users := []clioutput.CliUser{}
	for range normalizeMaxPages(opts.MaxPages) {
		page, err := pager.Next(ctx)
		if pager.Done(err) {
			return users, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		users = append(users, cliuser.CliUsersFromSlack(page.Users, false)...)
		if page.Cursor == "" {
			return users, false, nil
		}
		pager = page
	}
	return users, true, nil
}

func drainChannels(ctx context.Context, client *slackgo.Client, opts Options, types []string) ([]clioutput.CliChannel, bool, error) {
	channels := []clioutput.CliChannel{}
	cursor := ""
	for range normalizeMaxPages(opts.MaxPages) {
		page, nextCursor, err := client.GetConversationsContext(ctx, &slackgo.GetConversationsParameters{
			Types:           types,
			Limit:           normalizePageSize(opts.PageSize),
			Cursor:          cursor,
			ExcludeArchived: true,
		})
		if err != nil {
			return nil, false, err
		}
		channels = append(channels, clichannel.CliChannelsFromSlack(page)...)
		if nextCursor == "" {
			return channels, false, nil
		}
		cursor = nextCursor
	}
	return channels, true, nil
}

func marshalPayload(value any) (json.RawMessage, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func normalizePageSize(value int) int {
	if value <= 0 {
		return defaultPageSize
	}
	if value > 200 {
		return 200
	}
	return value
}

func normalizeMaxPages(value int) int {
	if value <= 0 {
		return defaultMaxPages
	}
	return value
}

type fetchError struct {
	err clioutput.CLIError
}

func (e fetchError) Error() string {
	return e.err.Message
}

func cliErrorFromCacheError(ctx context.Context, err error) clioutput.CLIError {
	var fetchErr fetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.err
	}
	return clioutput.CliErrorFromSlack(ctx, err)
}

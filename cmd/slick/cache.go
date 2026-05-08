package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

const (
	slackMetadataCacheTTLMinutes = 24 * 60
	defaultCachePageSize         = 200
	defaultCacheMaxPages         = 20
)

var cacheResources = []string{"users", "channels"}

type cacheOptions struct {
	Refresh    bool
	TTLMinutes int
	PageSize   int
	MaxPages   int
}

type cachedUsersPayload struct {
	Users     []cliUser `json:"users"`
	Truncated bool      `json:"truncated,omitempty"`
}

type cachedChannelsPayload struct {
	Channels  []cliChannel `json:"channels"`
	Truncated bool         `json:"truncated,omitempty"`
}

type cacheUsersData struct {
	Profile   string    `json:"profile"`
	Users     []cliUser `json:"users"`
	Count     int       `json:"count"`
	FromCache bool      `json:"from_cache"`
	FetchedAt string    `json:"fetched_at"`
	Truncated bool      `json:"truncated,omitempty"`
}

type cacheChannelsData struct {
	Profile   string       `json:"profile"`
	Channels  []cliChannel `json:"channels"`
	Count     int          `json:"count"`
	FromCache bool         `json:"from_cache"`
	FetchedAt string       `json:"fetched_at"`
	Truncated bool         `json:"truncated,omitempty"`
}

type cacheClearData struct {
	Profile      string `json:"profile"`
	Resource     string `json:"resource,omitempty"`
	Removed      bool   `json:"removed,omitempty"`
	RemovedCount int    `json:"removed_count,omitempty"`
}

func newCacheCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Prime and inspect local Slack metadata caches",
	}
	cmd.AddCommand(newCacheUsersCommand(runtime))
	cmd.AddCommand(newCacheChannelsCommand(runtime))
	cmd.AddCommand(newCacheClearCommand(runtime))
	return cmd
}

func newCacheUsersCommand(runtime *RootRuntime) *cobra.Command {
	opts := defaultCacheOptions()
	cmd := &cobra.Command{
		Use:          "users",
		Short:        "Cache and print active Slack users",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCacheUsers(cmd, runtime, opts)
		},
	}
	addCacheFlags(cmd, &opts)
	return cmd
}

func newCacheChannelsCommand(runtime *RootRuntime) *cobra.Command {
	opts := defaultCacheOptions()
	cmd := &cobra.Command{
		Use:          "channels",
		Short:        "Cache and print active Slack conversations",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCacheChannels(cmd, runtime, opts)
		},
	}
	addCacheFlags(cmd, &opts)
	return cmd
}

func newCacheClearCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "clear [resource]",
		Short:        "Clear local Slack metadata caches",
		Args:         cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs:    cacheResources,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear(cmd, runtime, args)
		},
	}
	return cmd
}

func defaultCacheOptions() cacheOptions {
	return cacheOptions{
		TTLMinutes: slackMetadataCacheTTLMinutes,
		PageSize:   defaultCachePageSize,
		MaxPages:   defaultCacheMaxPages,
	}
}

func addCacheFlags(cmd *cobra.Command, opts *cacheOptions) {
	cmd.Flags().BoolVarP(&opts.Refresh, "refresh", "r", false, "Force a fetch even when the cache is fresh")
	cmd.Flags().IntVarP(&opts.TTLMinutes, "ttl-minutes", "T", slackMetadataCacheTTLMinutes, "Freshness window before automatic refresh")
	cmd.Flags().IntVarP(&opts.PageSize, "page-size", "s", defaultCachePageSize, "Slack page size while priming")
	cmd.Flags().IntVarP(&opts.MaxPages, "max-pages", "N", defaultCacheMaxPages, "Maximum Slack pages to fetch")
}

func runCacheUsers(cmd *cobra.Command, runtime *RootRuntime, opts cacheOptions) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	data, fromCache, fetchedAt, err := cacheReadOrFetch(profile.Name, "users", opts.TTL(), opts.Refresh, func() (json.RawMessage, error) {
		return fetchUsersForCache(cmd, runtime, profile, opts)
	})
	if err != nil {
		return writeCommandError(ctx, cacheCLIError(cmd.Context(), err))
	}
	var payload cachedUsersPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return writeCommandError(ctx, validationCLIError(fmt.Sprintf("cache.users: decode cached payload: %v", err)))
	}
	return ctx.WriteResult("cache.users", cacheUsersData{
		Profile:   profile.Name,
		Users:     payload.Users,
		Count:     len(payload.Users),
		FromCache: fromCache,
		FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		Truncated: payload.Truncated,
	})
}

func runCacheChannels(cmd *cobra.Command, runtime *RootRuntime, opts cacheOptions) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	data, fromCache, fetchedAt, err := cacheReadOrFetch(profile.Name, "channels", opts.TTL(), opts.Refresh, func() (json.RawMessage, error) {
		return fetchChannelsForCache(cmd, runtime, profile, opts)
	})
	if err != nil {
		return writeCommandError(ctx, cacheCLIError(cmd.Context(), err))
	}
	var payload cachedChannelsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return writeCommandError(ctx, validationCLIError(fmt.Sprintf("cache.channels: decode cached payload: %v", err)))
	}
	return ctx.WriteResult("cache.channels", cacheChannelsData{
		Profile:   profile.Name,
		Channels:  payload.Channels,
		Count:     len(payload.Channels),
		FromCache: fromCache,
		FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		Truncated: payload.Truncated,
	})
}

func runCacheClear(cmd *cobra.Command, runtime *RootRuntime, args []string) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if len(args) == 0 {
		count, err := slackcache.ClearProfile(profile.Name)
		if err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
		return ctx.WriteResult("cache.clear", cacheClearData{Profile: profile.Name, RemovedCount: count})
	}
	resource := args[0]
	removed, err := slackcache.Clear(profile.Name, resource)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	return ctx.WriteResult("cache.clear", cacheClearData{Profile: profile.Name, Resource: resource, Removed: removed})
}

func cacheReadOrFetch(profile, resource string, ttl time.Duration, refresh bool, fetch func() (json.RawMessage, error)) (json.RawMessage, bool, time.Time, error) {
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

func fetchUsersForCache(cmd *cobra.Command, runtime *RootRuntime, profile config.WorkspaceProfile, opts cacheOptions) (json.RawMessage, error) {
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return nil, cacheFetchError{err: authCLIError(err.Error())}
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("users:read")); err != nil {
		return nil, cacheFetchError{err: cliErrorFromSlack(cmd.Context(), err)}
	}
	users, truncated, err := drainUsersForCache(cmd.Context(), client, opts)
	if err != nil {
		return nil, err
	}
	return marshalCachePayload(cachedUsersPayload{Users: users, Truncated: truncated})
}

func fetchChannelsForCache(cmd *cobra.Command, runtime *RootRuntime, profile config.WorkspaceProfile, opts cacheOptions) (json.RawMessage, error) {
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return nil, cacheFetchError{err: authCLIError(err.Error())}
	}
	types := []string{"public_channel", "private_channel", "im", "mpim"}
	if err := requireSlackScopes(cmd.Context(), client, conversationReadScopeRequirement(types)); err != nil {
		return nil, cacheFetchError{err: cliErrorFromSlack(cmd.Context(), err)}
	}
	channels, truncated, err := drainChannelsForCache(cmd.Context(), client, opts, types)
	if err != nil {
		return nil, err
	}
	return marshalCachePayload(cachedChannelsPayload{Channels: channels, Truncated: truncated})
}

func drainUsersForCache(ctx context.Context, client *slackgo.Client, opts cacheOptions) ([]cliUser, bool, error) {
	pager := client.GetUsersPaginated(slackgo.GetUsersOptionLimit(normalizeCachePageSize(opts.PageSize)))
	users := []cliUser{}
	for range normalizeCacheMaxPages(opts.MaxPages) {
		page, err := pager.Next(ctx)
		if pager.Done(err) {
			return users, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		users = append(users, cliUsersFromSlack(page.Users, false)...)
		if page.Cursor == "" {
			return users, false, nil
		}
		pager = page
	}
	return users, true, nil
}

func drainChannelsForCache(ctx context.Context, client *slackgo.Client, opts cacheOptions, types []string) ([]cliChannel, bool, error) {
	channels := []cliChannel{}
	cursor := ""
	for range normalizeCacheMaxPages(opts.MaxPages) {
		page, nextCursor, err := client.GetConversationsContext(ctx, &slackgo.GetConversationsParameters{
			Types:           types,
			Limit:           normalizeCachePageSize(opts.PageSize),
			Cursor:          cursor,
			ExcludeArchived: true,
		})
		if err != nil {
			return nil, false, err
		}
		channels = append(channels, cliChannelsFromSlack(page).Channels...)
		if nextCursor == "" {
			return channels, false, nil
		}
		cursor = nextCursor
	}
	return channels, true, nil
}

func marshalCachePayload(value any) (json.RawMessage, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func (o cacheOptions) TTL() time.Duration {
	if o.TTLMinutes <= 0 {
		return 0
	}
	return time.Duration(o.TTLMinutes) * time.Minute
}

func normalizeCachePageSize(value int) int {
	if value <= 0 {
		return defaultCachePageSize
	}
	if value > 200 {
		return 200
	}
	return value
}

func normalizeCacheMaxPages(value int) int {
	if value <= 0 {
		return defaultCacheMaxPages
	}
	return value
}

type cacheFetchError struct {
	err CLIError
}

func (e cacheFetchError) Error() string {
	return e.err.Message
}

func cacheCLIError(ctx context.Context, err error) CLIError {
	var fetchErr cacheFetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.err
	}
	return cliErrorFromSlack(ctx, err)
}

// Package bookmark manages channel link bookmarks.
package bookmark

import (
	"errors"
	"net/url"
	"strings"

	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	slack "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type result struct {
	Channel    string            `json:"channel"`
	BookmarkID string            `json:"bookmark_id,omitempty"`
	Bookmark   *slack.Bookmark   `json:"bookmark,omitempty"`
	Bookmarks  []slack.Bookmark  `json:"bookmarks,omitzero"`
	Changes    map[string]string `json:"changes,omitempty"`
	DryRun     bool              `json:"dry_run,omitempty"`
}

func (d result) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	items := d.Bookmarks
	if d.Bookmark != nil {
		items = []slack.Bookmark{*d.Bookmark}
	}
	if items != nil {
		for _, item := range items {
			c.ResultEvent(command).Str("channel", d.Channel).Str("id", item.ID).Str("title", item.Title).Str("link", item.Link).Msg("Bookmark")
		}
		if len(items) == 0 {
			c.ResultEvent(command).Str("channel", d.Channel).Msg("No bookmarks")
		}
		return nil
	}
	c.ResultEvent(command).Str("channel", d.Channel).Str("bookmark_id", d.BookmarkID).Any("changes", d.Changes).Bool("dry_run", d.DryRun).Msg(clioutput.ActionLabel(command))
	return nil
}

// NewCommand returns the bookmark command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	parent := &cobra.Command{Use: "bookmark", Short: "Manage channel link bookmarks"}
	for _, action := range []string{"list", "add", "edit", "remove"} {
		cmd := &cobra.Command{Use: action, Short: action + " channel bookmarks", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error { return run(cmd, runtime, action) }}
		cmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
		if action == "edit" || action == "remove" {
			cmd.Flags().StringP("id", "i", "", "Bookmark ID")
		}
		if action == "add" || action == "edit" {
			cmd.Flags().StringP("title", "t", "", "Bookmark title")
			cmd.Flags().StringP("link", "l", "", "HTTP or HTTPS URL")
			cmd.Flags().StringP("emoji", "e", "", "Emoji name; pass an empty value on edit to clear it")
		}
		parent.AddCommand(cmd)
	}
	return parent
}

func run(cmd *cobra.Command, runtime *cliruntime.RootRuntime, action string) error {
	c, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	d := result{Channel: strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))}
	validation := ""
	if d.Channel == "" {
		validation = "channel is required"
	}
	if action == "edit" || action == "remove" {
		d.BookmarkID, _ = cmd.Flags().GetString("id")
		d.BookmarkID = strings.TrimSpace(d.BookmarkID)
		if d.BookmarkID == "" {
			validation = "id is required"
		}
	}
	if action == "add" || action == "edit" {
		d.Changes = map[string]string{}
		for _, name := range []string{"title", "link", "emoji"} {
			if cmd.Flags().Changed(name) {
				value, _ := cmd.Flags().GetString(name)
				d.Changes[name] = strings.TrimSpace(value)
			}
		}
		if action == "add" && (d.Changes["title"] == "" || d.Changes["link"] == "") {
			validation = "title and link are required"
		}
		if action == "edit" && len(d.Changes) == 0 {
			validation = "provide at least one of title, link or emoji"
		}
		if title, exists := d.Changes["title"]; exists && title == "" {
			validation = "title must not be empty"
		}
		if link, exists := d.Changes["link"]; exists {
			u, e := url.Parse(link)
			if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
				validation = "link must be an HTTP or HTTPS URL without credentials"
			}
		}
	}
	if validation != "" {
		return clioutput.WriteCommandError(c, clioutput.ValidationCLIError(validation))
	}
	if action != "list" && cliruntime.DryRun(cmd) {
		d.DryRun = true
		return c.WriteResult(c.Command, d)
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(c, slackclient.CLIError(cmd.Context(), err))
	}
	scope := "bookmarks:write"
	if action == "list" {
		scope = "bookmarks:read"
	}
	if err = cliscope.Require(cmd.Context(), client, cliscope.AllOf(scope)); err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	var item slack.Bookmark
	switch action {
	case "list":
		d.Bookmarks, err = client.ListBookmarksContext(cmd.Context(), d.Channel)
		if d.Bookmarks == nil {
			d.Bookmarks = []slack.Bookmark{}
		}
	case "add":
		item, err = client.AddBookmarkContext(cmd.Context(), d.Channel, slack.AddBookmarkParameters{Title: d.Changes["title"], Type: "link", Link: d.Changes["link"], Emoji: d.Changes["emoji"]})
	case "edit":
		params := slack.EditBookmarkParameters{Link: d.Changes["link"]}
		if value, exists := d.Changes["title"]; exists {
			params.Title = &value
		}
		if value, exists := d.Changes["emoji"]; exists {
			params.Emoji = &value
		}
		item, err = client.EditBookmarkContext(cmd.Context(), d.Channel, d.BookmarkID, params)
	case "remove":
		err = client.RemoveBookmarkContext(cmd.Context(), d.Channel, d.BookmarkID)
	}
	if action == "add" || action == "edit" {
		if err == nil && (strings.TrimSpace(item.ID) == "" || (action == "edit" && item.ID != d.BookmarkID) || item.ChannelID != d.Channel) {
			err = errors.New("slack returned a missing or mismatched bookmark")
		}
		d.Bookmark = &item
	}
	if err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	return c.WriteResult(c.Command, d)
}

// Package file implements the `slick file` cobra command tree, which uploads
// files to Slack channels and threads.
package file

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/blockkit"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// UploadData is the result returned by `slick file upload`.
type UploadData struct {
	File                              Info                           `json:"file"`
	Channel                           string                         `json:"channel"`
	clioutput.SlackConversationFields                                // channel_name, channel_hr, channel_url
	Attribution                       bool                           `json:"attribution"`
	DryRun                            bool                           `json:"dry_run,omitempty"`
	ChannelRef                        clioutput.SlackConversationRef `json:"-"`
}

var (
	_ clioutput.PlainRenderer  = UploadData{}
	_ clioutput.ResultEnricher = UploadData{}
)

func (d UploadData) EnrichResult(c *clioutput.CommandContext) any {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	d.SlackConversationFields = c.SlackConversationFields(ref)
	return d
}

func (d UploadData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	event := c.ResultEventWithStyles(command,
		clioutput.EntityFieldStyle("channel", ref.ID),
		// Seed stays "file:<id>" so file IDs hash-color into a distinct
		// family from channel/user/etc.; the rendered field key is "id".
		clioutput.HashedFieldStyle("id", "file:"+d.File.ID),
	)
	if clioutput.ShouldShowSlackConversationField(ref, clog.IsVerbose()) {
		event = clioutput.AddSlackConversationField(event, c, "channel", ref)
	}
	event = event.
		Str("id", d.File.ID).
		Str("name", d.File.Name).
		Bool("attribution", d.Attribution).
		Bool("dry_run", d.DryRun)
	if d.File.Permalink != nil {
		event = event.Link("permalink", *d.File.Permalink, c.HyperlinkText(climessage.PermalinkText(*d.File.Permalink)))
	}
	event = event.Str("size", human.FormatIECBytes(float64(d.File.Size)))
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

// Info captures the relevant Slack file metadata returned to the caller.
type Info struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	Size      int     `json:"size,omitempty"`
	Permalink *string `json:"permalink,omitempty"`
}

// NewCommand returns the `slick file` parent command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	fileCmd := &cobra.Command{
		Use:    "file",
		Short:  "Upload Slack files",
		Hidden: true,
	}

	var filePath string
	var filename string
	var title string
	var message string
	var blocks bool
	var thread string
	uploadCmd := &cobra.Command{
		Use:          "upload",
		Short:        "Upload a file to Slack",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpload(cmd, runtime, uploadOptions{
				FilePath: filePath,
				Filename: filename,
				Title:    title,
				Message:  message,
				Blocks:   blocks,
				Thread:   thread,
				DryRun:   cliruntime.DryRun(cmd),
			})
		},
	}
	uploadCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	uploadCmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to upload or - for stdin")
	uploadCmd.Flags().StringVarP(&filename, "filename", "N", "", "Filename for stdin or override")
	uploadCmd.Flags().StringVarP(&title, "title", "T", "", "Slack file title")
	uploadCmd.Flags().StringVarP(&message, "message", "m", "", "Initial comment")
	uploadCmd.Flags().BoolVarP(&blocks, "blocks", "b", false, "Treat upload message as raw Block Kit JSON")
	uploadCmd.Flags().StringVarP(&thread, "thread", "t", "", "Thread timestamp")
	cliruntime.RegisterAttributionFlags(uploadCmd)
	fileCmd.AddCommand(uploadCmd)

	return fileCmd
}

type uploadOptions struct {
	FilePath string
	Filename string
	Title    string
	Message  string
	Blocks   bool
	Thread   string
	DryRun   bool
}

func runUpload(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts uploadOptions) error {
	ctx, profile, attribution, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	content, filename, err := readUploadSource(runtime.Stdin, opts.FilePath, opts.Filename)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	blocks, err := uploadMessageBlocks(opts.Message, opts.Blocks, attribution)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	ctx.StderrLogger().Debug().Parts(clog.PartMessage).Msg("uploading file")
	if opts.DryRun {
		data := UploadData{
			File:        Info{ID: "dry-run", Name: filename, Size: len(content)},
			Channel:     channel,
			Attribution: attribution.Enabled,
			DryRun:      true,
		}
		data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), nil, profile.Name, channel)
		return ctx.WriteResult("file.upload", data)
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("files:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	params := slackgo.UploadFileParameters{
		Channel:         channel,
		Filename:        filename,
		Title:           opts.Title,
		ThreadTimestamp: opts.Thread,
		Reader:          bytes.NewReader(content),
		FileSize:        len(content),
	}
	if len(blocks) > 0 {
		params.Blocks = slackgo.Blocks{BlockSet: blocks}
	} else {
		params.InitialComment = opts.Message
	}
	file, err := client.UploadFileContext(cmd.Context(), params)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	fileName := cliutil.FirstNonEmpty(file.Title, filename)
	var filePermalink *string
	if info, _, _, infoErr := client.GetFileInfoContext(cmd.Context(), file.ID, 0, 0); infoErr == nil && info != nil {
		filePermalink = cliutil.StringPtr(info.Permalink)
	}
	data := UploadData{
		File:        Info{ID: file.ID, Name: fileName, Size: len(content), Permalink: filePermalink},
		Channel:     channel,
		Attribution: attribution.Enabled,
	}
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, channel)
	return ctx.WriteResult("file.upload", data)
}

func uploadMessageBlocks(message string, raw bool, attribution agent.Attribution) ([]slackgo.Block, error) {
	if strings.TrimSpace(message) == "" && !attribution.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(message) == "" {
		return []slackgo.Block{blockkit.AttributionBlockWithMessage(attribution.Emoji, attribution.Message)}, nil
	}
	return climessage.ComposeBlocks(message, raw, attribution)
}

func readUploadSource(stdin io.Reader, filePath, filename string) ([]byte, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, "", errors.New("file is required")
	}
	if filePath == "-" {
		if filename == "" {
			return nil, "", errors.New("filename is required when reading file content from stdin")
		}
		content, err := io.ReadAll(stdin)
		return content, filename, err
	}
	expandedPath := human.ExpandPath(filePath)
	content, err := os.ReadFile(expandedPath) //nolint:gosec // File upload intentionally reads the caller-supplied path.
	if err != nil {
		return nil, "", err
	}
	if filename == "" {
		filename = filepath.Base(expandedPath)
	}
	return content, filename, nil
}

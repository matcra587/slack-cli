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
	xfilepath "github.com/gechr/x/filepath"
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
	Title     string  `json:"title,omitempty"`
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
	uploadCmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
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

func runUpload(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts uploadOptions) (resultErr error) {
	ctx, profile, attribution, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	channel = strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	content, filename, err := readUploadSource(runtime.Stdin, opts.FilePath, opts.Filename)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	if len(content) == 0 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("upload file must not be empty"))
	}
	if strings.TrimSpace(filename) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("filename must not be blank"))
	}
	if strings.ContainsAny(filename, "\x00\r\n") {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("filename must not contain NUL or line breaks"))
	}
	blocks, err := uploadMessageBlocks(opts.Message, opts.Blocks, attribution)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	ctx.StderrLogger().Debug().Parts(clog.PartMessage).Msg("uploading file")
	if opts.DryRun {
		data := UploadData{
			File:        Info{ID: "dry-run", Name: filename, Title: opts.Title, Size: len(content)},
			Channel:     channel,
			Attribution: attribution.Enabled,
			DryRun:      true,
		}
		data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), nil, profile.Name, channel)
		return ctx.WriteResult("file.upload", data)
	}
	ctx.ReportProgramStatus(clog.ProgramStateWorking, "", "Preparing file upload")
	defer func() { ctx.FinishProgramStatus(resultErr) }()
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
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
	if opts.Blocks || len(blocks) > 0 {
		params.Blocks = slackgo.Blocks{BlockSet: blocks}
	} else {
		params.InitialComment = opts.Message
	}
	file, err := uploadFile(cmd.Context(), client, params, func(message string) {
		ctx.ReportProgramStatus(clog.ProgramStateWorking, "", message)
	})
	if err != nil {
		cliErr := clioutput.CliErrorFromSlack(cmd.Context(), err, "")
		if progress, ok := errors.AsType[*uploadFailure](err); ok {
			details := map[string]any{"stage": progress.stage}
			if progress.fileID != "" {
				details["file_id"] = progress.fileID
			}
			cliErr = cliErr.WithDetails(details)
		}
		return clioutput.WriteCommandError(ctx, cliErr)
	}
	fileName := filename
	fileTitle := cliutil.FirstNonEmpty(file.Title, opts.Title)
	var filePermalink *string
	if info, _, _, infoErr := client.GetFileInfoContext(cmd.Context(), file.ID, 1, 1); infoErr == nil && info != nil && info.ID == file.ID {
		filePermalink = cliutil.StringPtr(info.Permalink)
		fileName = cliutil.FirstNonEmpty(info.Name, fileName)
		fileTitle = cliutil.FirstNonEmpty(info.Title, fileTitle)
	}
	data := UploadData{
		File:        Info{ID: file.ID, Name: fileName, Title: fileTitle, Size: len(content), Permalink: filePermalink},
		Channel:     channel,
		Attribution: attribution.Enabled,
	}
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, channel)
	return ctx.WriteResult("file.upload", data)
}

func uploadMessageBlocks(message string, raw bool, attribution agent.Attribution) ([]slackgo.Block, error) {
	if raw {
		return climessage.ComposeBlocks(message, true, attribution)
	}
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
		if stdin == nil {
			return nil, "", errors.New("stdin is unavailable")
		}
		content, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", errors.New("could not read upload data from stdin")
		}
		return content, filename, nil
	}
	expandedPath := xfilepath.Expand(filePath)
	content, err := os.ReadFile(expandedPath) //nolint:gosec // File upload intentionally reads the caller-supplied path.
	if err != nil {
		return nil, "", err
	}
	if filename == "" {
		filename = filepath.Base(expandedPath)
	}
	return content, filename, nil
}

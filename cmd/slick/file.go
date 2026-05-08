package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	"github.com/matcra587/slack-cli/pkg/blockkit"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

func newFileCommand(runtime *RootRuntime) *cobra.Command {
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
	var dryRun bool
	uploadCmd := &cobra.Command{
		Use:          "upload",
		Short:        "Upload a file to Slack",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFileUpload(cmd, runtime, uploadOptions{
				FilePath: filePath,
				Filename: filename,
				Title:    title,
				Message:  message,
				Blocks:   blocks,
				Thread:   thread,
				DryRun:   dryRun,
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
	uploadCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without uploading")
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

func runFileUpload(cmd *cobra.Command, runtime *RootRuntime, opts uploadOptions) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}
	content, filename, err := readUploadSource(runtime.Stdin, opts.FilePath, opts.Filename)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	blocks, err := uploadMessageBlocks(opts.Message, opts.Blocks, attribution)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}

	ctx.stderrLogger().Debug().Parts(clog.PartMessage).Msg("uploading file")
	if opts.DryRun {
		return ctx.WriteResult("file.upload", uploadFileResult{
			File:    cliFile{ID: "dry-run", Name: filename, Size: len(content)},
			Channel: channel,
			DryRun:  true,
		})
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("files:write")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
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
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	fileName := firstNonEmpty(file.Title, filename)
	var filePermalink *string
	if info, _, _, infoErr := client.GetFileInfoContext(cmd.Context(), file.ID, 0, 0); infoErr == nil && info != nil {
		filePermalink = stringPtr(info.Permalink)
	}
	return ctx.WriteResult("file.upload", uploadFileResult{
		File:    cliFile{ID: file.ID, Name: fileName, Size: len(content), Permalink: filePermalink},
		Channel: channel,
	})
}

func uploadMessageBlocks(message string, raw bool, attribution Attribution) ([]slackgo.Block, error) {
	if strings.TrimSpace(message) == "" && !attribution.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(message) == "" {
		return []slackgo.Block{blockkit.AttributionBlockWithMessage(attribution.Emoji, attribution.Message)}, nil
	}
	return composeBlocks(message, raw, attribution)
}

func readUploadSource(stdin io.Reader, filePath, filename string) ([]byte, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, "", errString("file is required")
	}
	if filePath == "-" {
		if filename == "" {
			return nil, "", errString("filename is required when reading file content from stdin")
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

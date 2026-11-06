package file

import (
	"context"
	"errors"

	slackgo "github.com/slack-go/slack"
)

// Keep the acknowledged file ID through each step. A failed completion reply
// does not prove whether Slack completed the upload, so never retry or delete it.
func uploadFile(ctx context.Context, client *slackgo.Client, params slackgo.UploadFileParameters) (*slackgo.FileSummary, error) {
	prepared, err := client.GetUploadURLExternalContext(ctx, slackgo.GetUploadURLExternalParameters{
		FileName:    params.Filename,
		FileSize:    params.FileSize,
		AltTxt:      params.AltTxt,
		SnippetType: params.SnippetType,
	})
	if err != nil {
		return nil, &uploadFailure{stage: "prepare", cause: err}
	}
	if prepared == nil || !prepared.Ok || prepared.FileID == "" || prepared.UploadURL == "" {
		return nil, &uploadFailure{stage: "prepare", cause: errors.New("slack did not acknowledge upload preparation")}
	}
	id := prepared.FileID
	if err := client.UploadToURL(ctx, slackgo.UploadToURLParameters{
		UploadURL: prepared.UploadURL,
		Reader:    params.Reader,
		File:      params.File,
		Content:   params.Content,
		Filename:  params.Filename,
	}); err != nil {
		return nil, &uploadFailure{stage: "transfer", fileID: id, cause: err}
	}
	completed, err := client.CompleteUploadExternalContext(ctx, slackgo.CompleteUploadExternalParameters{
		Files:           []slackgo.FileSummary{{ID: id, Title: params.Title}},
		Channel:         params.Channel,
		Channels:        params.Channels,
		InitialComment:  params.InitialComment,
		Blocks:          params.Blocks,
		ThreadTimestamp: params.ThreadTimestamp,
	})
	if err != nil {
		return nil, &uploadFailure{stage: "complete", fileID: id, cause: err}
	}
	if completed == nil || !completed.Ok || len(completed.Files) != 1 || completed.Files[0].ID != id {
		return nil, &uploadFailure{stage: "complete", fileID: id, cause: errors.New("slack returned an inconsistent upload completion")}
	}
	return &completed.Files[0], nil
}

type uploadFailure struct {
	stage, fileID string
	cause         error
}

func (e *uploadFailure) Error() string    { return "file upload failed during " + e.stage }
func (e *uploadFailure) GoString() string { return e.Error() }
func (e *uploadFailure) Unwrap() error    { return e.cause }

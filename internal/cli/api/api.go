// Package api exposes Slack's published Web API methods through slick.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/gechr/clog"
	publicapi "github.com/matcra587/slack-cli/api"
	"github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// NewCommand creates the API discovery and execution commands.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	command := &cobra.Command{Use: "api", Short: "Discover and call Slack Web API methods"}
	methods := &cobra.Command{Use: "methods [prefix]", Short: "List published methods and execution policy", Args: cobra.MaximumNArgs(1)}
	methods.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cliruntime.LocalContext(cmd, runtime, "")
		prefix := ""
		if len(args) > 0 {
			prefix = strings.ToLower(args[0])
		}
		data := MethodsData{Methods: []slackcatalog.Method{}, Provenance: slackcatalog.Provenance()}
		for _, method := range slackcatalog.All() {
			if strings.HasPrefix(strings.ToLower(method.Name), prefix) {
				data.Methods = append(data.Methods, method)
			}
		}
		return ctx.WriteResult("api.methods", data)
	}
	describe := &cobra.Command{Use: "describe METHOD", Short: "Show a method's arguments, scopes, and execution policy", Args: cobra.ExactArgs(1)}
	describe.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cliruntime.LocalContext(cmd, runtime, "")
		method, ok := slackcatalog.Lookup(args[0])
		if !ok {
			return output.WriteCommandError(ctx, output.ValidationCLIError("unknown Slack API method"))
		}
		return ctx.WriteResult("api.describe", MethodData{Method: method})
	}
	command.AddCommand(methods, describe, newCallCommand(runtime))
	return command
}

type callOptions struct {
	input           string
	fields          []string
	force, paginate bool
	maxPages        int
	outputFile      string
}

func newCallCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := &callOptions{}
	cmd := &cobra.Command{Use: "call METHOD", Short: "Call a published Slack method without automatic retries", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVarP(&opts.input, "input", "f", "", "JSON object file, or - for stdin")
	cmd.Flags().StringArrayVarP(&opts.fields, "field", "e", nil, "Parameter name=value (strings; repeatable)")
	cmd.Flags().BoolVarP(&opts.force, "force", "F", false, "Authorize a method that can change Slack state")
	cmd.Flags().BoolVarP(&opts.paginate, "paginate", "p", false, "Fetch bounded pages for a reviewed read method")
	cmd.Flags().IntVarP(&opts.maxPages, "max-pages", "N", 10, "Maximum pages to fetch (1-1000)")
	cmd.Flags().StringVarP(&opts.outputFile, "output-file", "O", "", "Save the response in a new private file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runCall(cmd, runtime, opts, args[0]) }
	return cmd
}

func unsupported(method slackcatalog.Method) string {
	if method.Deprecated {
		return "this method is deprecated or retired"
	}
	if method.UnsupportedReason != "" {
		return method.UnsupportedReason
	}
	if method.Authentication != "bearer" && method.Authentication != "none" {
		return "this authentication flow requires a dedicated command"
	}
	if method.ResponseFormat != "json" {
		return "this response format requires a dedicated command"
	}
	return ""
}

func runCall(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts *callOptions, name string) (resultErr error) {
	local := cliruntime.LocalContext(cmd, runtime, "")
	fail := func(message string) error { return output.WriteCommandError(local, output.ValidationCLIError(message)) }
	method, known := slackcatalog.Lookup(name)
	if !known {
		return fail("unknown Slack API method; inspect api methods")
	}
	if reason := unsupported(method); reason != "" {
		return fail("method unavailable: " + reason)
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if !method.ReadOnly && !opts.force && !dryRun {
		return fail("this method may change Slack state; pass --force or --dry-run")
	}
	if opts.paginate && (!method.ReadOnly || (method.Pagination != "cursor" && method.Pagination != "page")) {
		return fail("--paginate requires a reviewed read method with documented pagination")
	}
	if opts.maxPages < 1 || opts.maxPages > 1000 {
		return fail("--max-pages must be between 1 and 1000")
	}
	if method.SensitiveResponse && opts.outputFile == "" {
		return fail("this method can return credentials or private download links; use --output-file")
	}
	params, err := readParameters(opts.input, opts.fields, runtime.Stdin)
	if err != nil {
		return fail(err.Error())
	}
	for _, required := range method.Required {
		value, ok := params[required]
		empty, isString := value.(string)
		if !ok || value == nil || (isString && empty == "") {
			return fail("missing required parameter: " + required)
		}
	}
	if dryRun {
		return local.WriteResult("api.call", CallData{Method: method.Name, DryRun: true, Parameters: params})
	}
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return output.WriteCommandError(ctx, output.ValidationCLIError("cannot resolve selected workspace"))
	}
	if runtime.TokenResolver == nil {
		return output.WriteCommandError(ctx, output.AuthCLIError("no credential resolver is available"))
	}
	if opts.paginate {
		ctx.ReportProgramStatus(clog.ProgramStateWorking, "", "Reading Slack API pages")
		defer func() { ctx.FinishProgramStatus(resultErr) }()
	}
	token, err := runtime.TokenResolver.ResolveToken(cmd.Context(), profile)
	if err != nil {
		return output.WriteCommandError(ctx, output.AuthCLIError("cannot resolve selected workspace credentials"))
	}
	noThrottle, _ := cmd.Flags().GetBool("no-throttle")
	client, err := publicapi.NewClientWithContext(cmd.Context(), publicapi.ClientOptions{AuthToken: token, APIURL: runtime.SlackBaseURL, HTTPClient: runtime.HTTPClient, NoThrottle: noThrottle})
	if err != nil {
		return output.WriteCommandError(ctx, output.AuthCLIError("cannot initialize Slack API client"))
	}
	var file *responseFile
	if opts.outputFile != "" {
		file, err = createResponseFile(opts.outputFile)
		if err != nil {
			return output.WriteCommandError(ctx, output.ValidationCLIError("output file must be a new writable path"))
		}
		defer file.cleanup()
	}
	data, callErr := callPages(cmd, client, method, params, opts, func(page int) {
		ctx.ReportProgramStatus(clog.ProgramStateWorking, "", "Reading Slack API page "+strconv.Itoa(page))
	})
	if callErr != nil {
		cliErr := safeCallError(cmd, callErr)
		if data.CompletedPages > 0 {
			cliErr = cliErr.WithDetails(map[string]any{"completed_pages": data.CompletedPages, "next_cursor": data.NextCursor, "next_page": data.NextPage})
		}
		return output.WriteCommandError(ctx, cliErr)
	}
	if file != nil {
		var body []byte
		if opts.paginate {
			body, err = json.Marshal(data)
		} else {
			body = data.Response
		}
		if err == nil {
			err = file.write(body)
		}
		if err != nil {
			return output.WriteCommandError(ctx, output.CLIError{Type: output.ErrorTypeIO, Message: "output_write_failed", ExitCode: output.ExitCodeIO})
		}
		data = CallData{Method: method.Name, OutputFile: opts.outputFile}
	}
	return ctx.WriteResult("api.call", data)
}

func safeCallError(cmd *cobra.Command, err error) output.CLIError {
	if apiErr, ok := errors.AsType[*publicapi.Error](err); ok {
		// Slack error codes are useful classifications, while provider-controlled
		// diagnostic text and scope strings must never become CLI diagnostics.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return output.CliErrorFromSlack(cmd.Context(), err, "api")
		}
		if rateErr, ok := errors.AsType[*slackgo.RateLimitedError](err); ok {
			return output.CliErrorFromSlack(cmd.Context(), rateErr, "api")
		}
		code := apiErr.Code
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`).MatchString(code) {
			code = "invalid_response"
		}
		clean := slackgo.SlackErrorResponse{Err: code}
		var remote slackgo.SlackErrorResponse
		if errors.As(err, &remote) && regexp.MustCompile(`^[a-z][a-z0-9_.]*:[a-z][a-z0-9_.]*(,[a-z][a-z0-9_.]*:[a-z][a-z0-9_.]*)*$`).MatchString(remote.Needed) {
			clean.Needed = remote.Needed
		}
		return output.CliErrorFromSlack(cmd.Context(), clean, "api")
	}
	if errors.Is(err, cmd.Context().Err()) && cmd.Context().Err() != nil {
		return output.CliErrorFromSlack(cmd.Context(), err, "api")
	}
	return output.CLIError{Type: output.ErrorTypeServer, Message: "Slack API request failed", ExitCode: output.ExitCodeServer}
}

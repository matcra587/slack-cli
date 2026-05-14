// Package health implements the `slick health` command tree for Slack service
// health and Web API reachability checks.
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

const (
	defaultSlackAPIBaseURL    = "https://slack.com/api/"
	defaultSlackStatusBaseURL = "https://slack-status.com/api/v2.0.0/"
	maxResponseBytes          = 5 << 20
)

type commandOptions struct {
	Service string
	Limit   int
}

type Client struct {
	HTTPClient         *http.Client
	SlackAPIBaseURL    string
	SlackStatusBaseURL string
}

type apiTestData struct {
	OK   bool           `json:"ok"`
	Args map[string]any `json:"args,omitempty"`
}

type apiTestResponse struct {
	slackgo.SlackResponse
	Args map[string]any `json:"args,omitempty"`
}

type currentResponse struct {
	Status          string        `json:"status"`
	DateCreated     string        `json:"date_created,omitempty"`
	DateUpdated     string        `json:"date_updated,omitempty"`
	ActiveIncidents []rawIncident `json:"active_incidents,omitempty"`
}

type rawIncident struct {
	ID          flexibleID `json:"id"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	URL         string     `json:"url"`
	DateCreated string     `json:"date_created"`
	DateUpdated string     `json:"date_updated"`
	Services    []string   `json:"services"`
	Notes       []rawNote  `json:"notes"`
}

type rawNote struct {
	Body        string `json:"body"`
	DateCreated string `json:"date_created"`
}

type flexibleID string

func (id *flexibleID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*id = flexibleID(text)
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		*id = flexibleID(number.String())
		return nil
	}
	return fmt.Errorf("invalid incident id")
}

type CurrentData struct {
	Status                   string                     `json:"status"`
	Service                  string                     `json:"service,omitempty"`
	DateCreated              string                     `json:"date_created,omitempty"`
	DateUpdated              string                     `json:"date_updated,omitempty"`
	ActiveIncidents          []clioutput.HealthIncident `json:"active_incidents"`
	ActiveIncidentCount      int                        `json:"active_incident_count"`
	TotalActiveIncidentCount int                        `json:"total_active_incident_count"`
}

type HistoryData struct {
	Service       string                     `json:"service,omitempty"`
	Limit         int                        `json:"limit,omitempty"`
	Incidents     []clioutput.HealthIncident `json:"incidents"`
	IncidentCount int                        `json:"incident_count"`
}

type CheckData struct {
	Healthy                  bool                       `json:"healthy"`
	Status                   string                     `json:"status"`
	APIOK                    bool                       `json:"api_ok"`
	Service                  string                     `json:"service,omitempty"`
	DateUpdated              string                     `json:"date_updated,omitempty"`
	ActiveIncidents          []clioutput.HealthIncident `json:"active_incidents"`
	ActiveIncidentCount      int                        `json:"active_incident_count"`
	TotalActiveIncidentCount int                        `json:"total_active_incident_count"`
}

var (
	_ clioutput.PlainRenderer = CurrentData{}
	_ clioutput.PlainRenderer = HistoryData{}
	_ clioutput.PlainRenderer = CheckData{}
	_ clioutput.PlainRenderer = apiTestData{}
)

// NewCommand returns the `slick health` parent command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	healthCmd := &cobra.Command{
		Use:          "health",
		Short:        "Check Slack service and Web API health",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	healthCmd.AddCommand(newCurrentCommand(runtime))
	healthCmd.AddCommand(newHistoryCommand(runtime))
	healthCmd.AddCommand(newAPITestCommand(runtime))
	healthCmd.AddCommand(newCheckCommand(runtime))
	return healthCmd
}

func newCurrentCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var opts commandOptions
	cmd := &cobra.Command{
		Use:          "current",
		Short:        "Show current Slack service status",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCurrent(cmd, runtime, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Service, "service", "s", "", "Filter incidents by Slack service")
	return cmd
}

func newHistoryCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := commandOptions{Limit: 20}
	cmd := &cobra.Command{
		Use:          "history",
		Short:        "List recent Slack service incidents",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHistory(cmd, runtime, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Service, "service", "s", "", "Filter incidents by Slack service")
	cmd.Flags().IntVarP(&opts.Limit, "limit", "L", opts.Limit, "Maximum incidents to return; 0 returns all")
	return cmd
}

func newAPITestCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "api-test",
		Short:        "Check Slack Web API reachability with api.test",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAPITest(cmd, runtime)
		},
	}
}

func newCheckCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var opts commandOptions
	cmd := &cobra.Command{
		Use:          "check",
		Short:        "Run Slack service and Web API health checks",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd, runtime, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Service, "service", "s", "", "Filter incidents by Slack service")
	return cmd
}

func runCurrent(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts commandOptions) error {
	ctx := cliruntime.LocalContext(cmd, runtime, "health")
	client := clientForRuntime(runtime)
	current, err := client.Current(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliError(cmd.Context(), err))
	}
	data := currentData(current, opts.Service)
	return ctx.WriteResult("health.current", data)
}

func runHistory(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts commandOptions) error {
	ctx := cliruntime.LocalContext(cmd, runtime, "health")
	if opts.Limit < 0 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("limit must be zero or positive"))
	}
	client := clientForRuntime(runtime)
	incidents, err := client.History(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliError(cmd.Context(), err))
	}
	filtered := filterIncidents(incidents, opts.Service)
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	data := HistoryData{
		Service:       cleanService(opts.Service),
		Limit:         opts.Limit,
		Incidents:     ensureIncidentSlice(filtered),
		IncidentCount: len(filtered),
	}
	return ctx.WriteResult("health.history", data)
}

func runAPITest(cmd *cobra.Command, runtime *cliruntime.RootRuntime) error {
	ctx := cliruntime.LocalContext(cmd, runtime, "health")
	client := clientForRuntime(runtime)
	data, err := client.APITest(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliError(cmd.Context(), err))
	}
	return ctx.WriteResult("health.api-test", data)
}

func runCheck(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts commandOptions) error {
	ctx := cliruntime.LocalContext(cmd, runtime, "health")
	client := clientForRuntime(runtime)
	api, err := client.APITest(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliError(cmd.Context(), err))
	}
	current, err := client.Current(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, cliError(cmd.Context(), err))
	}
	currentData := currentData(current, opts.Service)
	data := CheckData{
		Healthy:                  healthCheckOK(api.OK, currentData),
		Status:                   currentData.Status,
		APIOK:                    api.OK,
		Service:                  currentData.Service,
		DateUpdated:              currentData.DateUpdated,
		ActiveIncidents:          currentData.ActiveIncidents,
		ActiveIncidentCount:      currentData.ActiveIncidentCount,
		TotalActiveIncidentCount: currentData.TotalActiveIncidentCount,
	}
	return ctx.WriteResult("health.check", data)
}

func healthCheckOK(apiOK bool, current CurrentData) bool {
	if !apiOK {
		return false
	}
	if current.Service != "" {
		return current.ActiveIncidentCount == 0
	}
	return strings.EqualFold(current.Status, "ok") && current.ActiveIncidentCount == 0
}

func clientForRuntime(runtime *cliruntime.RootRuntime) Client {
	httpClient := runtime.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: runtime.Timeout}
	}
	return Client{
		HTTPClient:         httpClient,
		SlackAPIBaseURL:    runtime.SlackBaseURL,
		SlackStatusBaseURL: runtime.SlackStatusBaseURL,
	}
}

func (c Client) APITest(ctx context.Context) (apiTestData, error) {
	values := url.Values{}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiMethodURL(c.SlackAPIBaseURL, "api.test"), strings.NewReader(values.Encode()))
	if err != nil {
		return apiTestData{}, fmt.Errorf("build api.test request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var response apiTestResponse
	if err := c.doJSON(req, &response); err != nil {
		return apiTestData{}, fmt.Errorf("call api.test: %w", err)
	}
	if err := response.Err(); err != nil {
		return apiTestData{}, err
	}
	return apiTestData{OK: response.Ok, Args: response.Args}, nil
}

func (c Client) Current(ctx context.Context) (currentResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusEndpointURL(c.SlackStatusBaseURL, "current"), nil)
	if err != nil {
		return currentResponse{}, fmt.Errorf("build slack status current request: %w", err)
	}
	var response currentResponse
	if err := c.doJSON(req, &response); err != nil {
		return currentResponse{}, fmt.Errorf("call slack status current: %w", err)
	}
	return response, nil
}

func (c Client) History(ctx context.Context) ([]clioutput.HealthIncident, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusEndpointURL(c.SlackStatusBaseURL, "history"), nil)
	if err != nil {
		return nil, fmt.Errorf("build slack status history request: %w", err)
	}
	var response []rawIncident
	if err := c.doJSON(req, &response); err != nil {
		return nil, fmt.Errorf("call slack status history: %w", err)
	}
	return incidentsFromRaw(response), nil
}

func (c Client) doJSON(req *http.Request, out any) error {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func currentData(response currentResponse, service string) CurrentData {
	all := incidentsFromRaw(response.ActiveIncidents)
	filtered := filterIncidents(all, service)
	return CurrentData{
		Status:                   response.Status,
		Service:                  cleanService(service),
		DateCreated:              response.DateCreated,
		DateUpdated:              response.DateUpdated,
		ActiveIncidents:          ensureIncidentSlice(filtered),
		ActiveIncidentCount:      len(filtered),
		TotalActiveIncidentCount: len(all),
	}
}

func incidentsFromRaw(raw []rawIncident) []clioutput.HealthIncident {
	if len(raw) == 0 {
		return nil
	}
	out := make([]clioutput.HealthIncident, 0, len(raw))
	for _, incident := range raw {
		out = append(out, clioutput.HealthIncident{
			ID:          string(incident.ID),
			Title:       strings.TrimSpace(incident.Title),
			Type:        strings.TrimSpace(incident.Type),
			Status:      strings.TrimSpace(incident.Status),
			URL:         strings.TrimSpace(incident.URL),
			DateCreated: strings.TrimSpace(incident.DateCreated),
			DateUpdated: strings.TrimSpace(incident.DateUpdated),
			Services:    compactStrings(incident.Services),
			NoteCount:   len(incident.Notes),
		})
	}
	return out
}

func filterIncidents(incidents []clioutput.HealthIncident, service string) []clioutput.HealthIncident {
	service = strings.ToLower(cleanService(service))
	if service == "" {
		return incidents
	}
	return slices.Collect(func(yield func(clioutput.HealthIncident) bool) {
		for _, incident := range incidents {
			if incidentHasService(incident, service) && !yield(incident) {
				return
			}
		}
	})
}

func ensureIncidentSlice(incidents []clioutput.HealthIncident) []clioutput.HealthIncident {
	if incidents == nil {
		return []clioutput.HealthIncident{}
	}
	return incidents
}

func incidentHasService(incident clioutput.HealthIncident, service string) bool {
	return slices.ContainsFunc(incident.Services, func(candidate string) bool {
		return strings.EqualFold(strings.TrimSpace(candidate), service)
	})
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func cleanService(service string) string {
	return strings.TrimSpace(service)
}

func apiMethodURL(baseURL, method string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultSlackAPIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/" + method
	}
	return baseURL + "/api/" + method
}

func statusEndpointURL(baseURL, endpoint string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultSlackStatusBaseURL
	}
	return strings.TrimRight(baseURL, "/") + "/" + endpoint
}

func cliError(ctx context.Context, err error) clioutput.CLIError {
	var slackErr slackgo.SlackErrorResponse
	if errors.As(err, &slackErr) {
		return clioutput.CliErrorFromSlack(ctx, slackErr, "")
	}
	return clioutput.CliErrorFromSlack(ctx, err, "")
}

func (d CurrentData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	if len(d.ActiveIncidents) > 0 {
		return c.WriteHealthIncidents(command, d.ActiveIncidents, pagination)
	}
	clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "active_incidents")
	clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "total_active_incidents")
	event := c.ResultEvent(command).
		Str("service", d.Service).
		Str("updated", d.DateUpdated).
		Str("status", d.Status).
		Str("active_incidents", strconv.Itoa(d.ActiveIncidentCount)).
		Str("total_active_incidents", strconv.Itoa(d.TotalActiveIncidentCount))
	c.FinishResult(event, command, pagination)
	return nil
}

func (d HistoryData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	if len(d.Incidents) > 0 {
		return c.WriteHealthIncidents(command, d.Incidents, pagination)
	}
	clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "count")
	event := c.ResultEvent(command).
		Str("service", d.Service).
		Str("count", "0")
	c.FinishResult(event, command, pagination)
	return nil
}

func (d CheckData) WritePlain(c *clioutput.CommandContext, _ string, _ *clioutput.Pagination) error {
	clioutput.ApplyBoolValueStyle(c.StdoutLogger(), c.Theme, "healthy", d.Healthy)
	clioutput.ApplyBoolValueStyle(c.StdoutLogger(), c.Theme, "api_ok", d.APIOK)
	clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "active_incidents")
	clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "total_active_incidents")
	message := "Slack health degraded"
	if d.Healthy {
		message = "Slack health ok"
	}
	c.StdoutLogger().Info().
		Str("service", d.Service).
		Str("updated", d.DateUpdated).
		Str("healthy", strconv.FormatBool(d.Healthy)).
		Str("status", d.Status).
		Str("api_ok", strconv.FormatBool(d.APIOK)).
		Str("active_incidents", strconv.Itoa(d.ActiveIncidentCount)).
		Str("total_active_incidents", strconv.Itoa(d.TotalActiveIncidentCount)).
		Msg(message)
	return nil
}

func (d apiTestData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	clioutput.ApplyBoolValueStyle(c.StdoutLogger(), c.Theme, "ok", d.OK)
	event := c.ResultEvent(command).
		Str("ok", strconv.FormatBool(d.OK))
	c.FinishResult(event, command, pagination)
	return nil
}

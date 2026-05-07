package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	slackgo "github.com/slack-go/slack"
)

func exchangeOAuthUserCode(runtime *RootRuntime, clientID, code, redirectURI, verifier string) (*slackgo.OAuthV2Response, error) {
	values := url.Values{
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return postOAuthAccess(context.Background(), slackOAuthHTTPClient(runtime), runtime.SlackBaseURL, values)
}

func refreshOAuthUserToken(ctx context.Context, client *http.Client, baseURL, clientID, refreshToken string) (*slackgo.OAuthV2Response, error) {
	values := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return postOAuthAccess(ctx, client, baseURL, values)
}

func postOAuthAccess(ctx context.Context, client *http.Client, baseURL string, values url.Values) (*slackgo.OAuthV2Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := slackgo.APIURL
	if baseURL != "" {
		endpoint = slackAPIURL(baseURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"oauth.v2.access", strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, slackgo.StatusCodeError{Code: resp.StatusCode, Status: resp.Status}
	}

	response := &slackgo.OAuthV2Response{}
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return nil, err
	}
	if err := response.Err(); err != nil {
		var slackErr slackgo.SlackErrorResponse
		if errors.As(err, &slackErr) && slackErr.Err == "bad_client_secret" {
			return nil, errors.New("bad_client_secret: Slack treated this as a client-secret OAuth flow. Enable PKCE for the Slack app, or import a manifest with oauth_config.pkce_enabled=true; slack-cli local OAuth intentionally omits the client secret")
		}
		return nil, err
	}
	return response, nil
}

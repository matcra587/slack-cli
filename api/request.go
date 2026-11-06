package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/matcra587/slack-cli/internal/ratelimit"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
	slackgo "github.com/slack-go/slack"
)

var methodNamePattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)

// Call invokes a published JSON Web API method without automatic retries.
// CallWithContext is preferred when a request needs cancellation or a deadline.
func (c *Client) Call(method string, params map[string]any, response any) error {
	return c.CallWithContext(context.Background(), method, params, response)
}

// CallWithContext invokes a cataloged Web API method with bearer authentication.
// GET parameters use the query string; POST uses JSON when the method supports
// it, otherwise URL-encoded form data. Nested form arguments are JSON encoded.
//
// response must be nil or a non-nil pointer. Decoding replaces its value only
// after a complete successful response, preserving prior state on every error.
// Numbers decoded into interfaces/maps use json.Number. Generic calls make one
// attempt: callers must inspect remote state before retrying an uncertain write.
func (c *Client) CallWithContext(ctx context.Context, method string, params map[string]any, response any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.httpClient == nil {
		return callError("", "client_required", 0, nil)
	}
	contract, err := requestContract(method, params)
	if err != nil {
		return err
	}
	target, err := responseTarget(response)
	if err != nil {
		return callError(contract.Name, "invalid_response_target", 0, err)
	}
	req, err := c.buildRequest(ctx, contract, params)
	if err != nil {
		return callError(contract.Name, "invalid_arguments", 0, err)
	}
	if err := c.wait(ctx, contract.Name); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return callError(contract.Name, "request_failed", 0, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests {
		delay := retryAfter(resp.Header.Get("Retry-After"))
		failure := callError(contract.Name, "ratelimited", resp.StatusCode, &slackgo.RateLimitedError{RetryAfter: delay})
		failure.RetryAfter = delay
		return failure
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return callError(contract.Name, "http_error", resp.StatusCode, nil)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return callError(contract.Name, "response_read_failed", resp.StatusCode, err)
	}
	if int64(len(raw)) > c.maxResponseBytes {
		return callError(contract.Name, "response_too_large", resp.StatusCode, nil)
	}
	var envelope struct {
		OK               *bool                    `json:"ok"`
		Error            string                   `json:"error"`
		Needed           string                   `json:"needed"`
		Provided         string                   `json:"provided"`
		ResponseMetadata slackgo.ResponseMetadata `json:"response_metadata"`
	}
	if err := decodeResponse(raw, &envelope); err != nil || envelope.OK == nil {
		return callError(contract.Name, "invalid_response", resp.StatusCode, err)
	}
	if !*envelope.OK {
		if envelope.Error == "" {
			return callError(contract.Name, "invalid_response", resp.StatusCode, nil)
		}
		slackError := slackgo.SlackErrorResponse{
			Err: envelope.Error, Needed: envelope.Needed, Provided: envelope.Provided,
			ResponseMetadata: envelope.ResponseMetadata,
		}
		return callError(contract.Name, envelope.Error, resp.StatusCode, slackError)
	}
	if response != nil {
		if err := decodeResponse(raw, target.Interface()); err != nil {
			return callError(contract.Name, "invalid_response", resp.StatusCode, err)
		}
		reflect.ValueOf(response).Elem().Set(target.Elem())
	}
	return nil
}

func requestContract(name string, params map[string]any) (slackcatalog.Method, error) {
	if !methodNamePattern.MatchString(name) {
		return slackcatalog.Method{}, callError("", "invalid_method", 0, nil)
	}
	method, ok := slackcatalog.Lookup(name)
	if !ok {
		return slackcatalog.Method{}, callError("", "unknown_method", 0, nil)
	}
	if method.Deprecated || method.UnsupportedReason != "" || method.ResponseFormat != "json" ||
		(method.Authentication != "bearer" && method.Authentication != "none") ||
		(method.HTTPMethod != http.MethodGet && method.HTTPMethod != http.MethodPost) {
		return slackcatalog.Method{}, callError(method.Name, "unsupported_method", 0, nil)
	}
	for key := range params {
		if strings.EqualFold(key, "token") {
			return slackcatalog.Method{}, callError(method.Name, "token_parameter_forbidden", 0, nil)
		}
	}
	for _, name := range method.Required {
		value, ok := params[name]
		if !ok || value == nil {
			return slackcatalog.Method{}, callError(method.Name, "missing_required_argument", 0, nil)
		}
		if text, ok := value.(string); ok && text == "" {
			return slackcatalog.Method{}, callError(method.Name, "missing_required_argument", 0, nil)
		}
	}
	return method, nil
}

func responseTarget(response any) (reflect.Value, error) {
	if response == nil {
		return reflect.Value{}, nil
	}
	value := reflect.ValueOf(response)
	if value.Kind() != reflect.Pointer || value.IsNil() || !value.Elem().CanSet() {
		return reflect.Value{}, errors.New("response must be a non-nil pointer")
	}
	return reflect.New(value.Elem().Type()), nil
}

func (c *Client) buildRequest(ctx context.Context, method slackcatalog.Method, params map[string]any) (*http.Request, error) {
	endpoint := c.endpoint + method.Name
	var body io.Reader
	contentType := ""
	if method.HTTPMethod == http.MethodPost && slices.Contains(method.ContentTypes, "application/json") {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		if params == nil {
			raw = []byte("{}")
		}
		body = bytes.NewReader(raw)
		contentType = "application/json; charset=utf-8"
	} else {
		values, err := formValues(params)
		if err != nil {
			return nil, err
		}
		if method.HTTPMethod == http.MethodGet {
			if len(values) > 0 {
				endpoint += "?" + values.Encode()
			}
		} else {
			body = strings.NewReader(values.Encode())
			contentType = "application/x-www-form-urlencoded"
		}
	}
	req, err := http.NewRequestWithContext(ctx, method.HTTPMethod, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func formValues(params map[string]any) (url.Values, error) {
	values := url.Values{}
	for key, value := range params {
		if text, ok := value.(string); ok {
			values.Set(key, text)
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		values.Set(key, string(raw))
	}
	return values, nil
}

func decodeResponse(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("slack returned trailing response data")
	}
	return nil
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (c *Client) wait(ctx context.Context, method string) error {
	if c.noThrottle {
		return ctx.Err()
	}
	select {
	case c.throttleGate <- struct{}{}:
		defer func() { <-c.throttleGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.throttler.Wait(ctx, ratelimit.TierForMethod(method))
}

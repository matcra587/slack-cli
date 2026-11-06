package api_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKResponseGuardsFollowConfiguredAndNativeAPIPrefixes(t *testing.T) {
	for _, route := range []string{"root", "proxy", "native"} {
		for _, tc := range []struct {
			name, method, healthy string
			call                  func(context.Context, *slackgo.Client) error
		}{
			{"authentication", "auth.test", `{"ok":true,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, func(ctx context.Context, c *slackgo.Client) error { _, err := c.AuthTestContext(ctx); return err }},
			{"revocation with a different token", "auth.revoke", `{"ok":true,"revoked":true}`, func(ctx context.Context, c *slackgo.Client) error {
				_, err := c.SendAuthRevokeContext(ctx, "xoxp-synthetic-revoked-token")
				return err
			}},
			{"status", "users.profile.set", `{"ok":true}`, func(ctx context.Context, c *slackgo.Client) error {
				return c.SetUserCustomStatusContext(ctx, "Reviewing", ":memo:", 0)
			}},
			{"reaction", "reactions.add", `{"ok":true}`, func(ctx context.Context, c *slackgo.Client) error {
				return c.AddReactionContext(ctx, "+1", slackgo.ItemRef{Channel: "C_FIXTURE", Timestamp: "1746284582.000001"})
			}},
			{"upload URL", "files.getUploadURLExternal", `{"ok":true,"file_id":"F123","upload_url":"https://fixture.invalid/upload"}`, func(ctx context.Context, c *slackgo.Client) error {
				_, err := c.GetUploadURLExternalContext(ctx, slackgo.GetUploadURLExternalParameters{FileName: "fixture.txt", FileSize: 4})
				return err
			}},
			{"message", "chat.postMessage", `{"ok":true,"channel":"C_FIXTURE","ts":"1746284582.000001"}`, func(ctx context.Context, c *slackgo.Client) error {
				_, _, err := c.PostMessageContext(ctx, "C_FIXTURE", slackgo.MsgOptionText(strings.Repeat("Review ", 2048), false))
				return err
			}},
			{"history", "conversations.history", `{"ok":true,"messages":[]}`, func(ctx context.Context, c *slackgo.Client) error {
				_, err := c.GetConversationHistoryContext(ctx, &slackgo.GetConversationHistoryParameters{ChannelID: "C_FIXTURE"})
				return err
			}},
			{"lookup", "users.list", `{"ok":true,"members":[]}`, func(ctx context.Context, c *slackgo.Client) error { _, err := c.GetUsersContext(ctx); return err }},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				const token = "xoxp-synthetic-routing"
				prefix := "/api/"
				if route == "proxy" {
					prefix = "/proxy/api/"
				}
				if route == "native" {
					prefix = "/native/"
				}
				calls := 0
				transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != prefix+tc.method || r.URL.RawQuery != "" {
						t.Fatal("SDK changed the method or configured endpoint")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					wantToken := token
					if tc.method == "auth.revoke" {
						wantToken = "xoxp-synthetic-revoked-token"
					}
					if r.PostForm.Get("token") != wantToken {
						t.Fatal("SDK changed request authentication")
					}
					body := "{}"
					if calls == 2 {
						body = tc.healthy
					}
					if calls > 2 {
						t.Fatal("SDK unexpectedly retried a request")
					}
					return sdkRoutingResponse(r, body), nil
				})
				opts := api.SDKOptions{APIURL: "https://fixture.invalid" + strings.TrimSuffix(prefix, "/"), HTTPClient: &http.Client{Transport: transport}, NoThrottle: true}
				var native []slackgo.Option
				if route == "native" {
					opts.APIURL = "https://fixture.invalid"
					native = []slackgo.Option{slackgo.OptionAPIURL("https://fixture.invalid/native/")}
				}
				client := api.NewSDKClient(token, opts, native...)
				if err := tc.call(t.Context(), client); err == nil {
					t.Error("missing API acknowledgement was accepted behind a configured prefix")
				}
				if calls != 1 {
					t.Fatal("invalid acknowledgement was replayed")
				}
				if err := tc.call(t.Context(), client); err != nil || calls != 2 {
					t.Fatalf("explicit healthy retry did not recover: %v", err)
				}
			})
		}
	}
}

func TestSDKDownloadsWithMethodNamesRemainOpaque(t *testing.T) {
	for _, name := range []string{"report.bin", "reactions.get", "reactions.add", "reactions.remove"} {
		t.Run(name, func(t *testing.T) {
			const token = "xoxb-synthetic-download-routing"
			payload := []byte{0, 255, '{', '}', '\n'}
			calls := 0
			transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/files-pri/T/F/"+name || r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatal("download changed its method, target or authentication")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(bytes.NewReader(payload)), Request: r}, nil
			})
			client := api.NewSDKClient(token, api.SDKOptions{APIURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: transport}})
			var dst bytes.Buffer
			dst.WriteString("prior:")
			if err := client.GetFileContext(t.Context(), "https://fixture.invalid/files-pri/T/F/"+name, &dst); err != nil || calls != 1 || !bytes.Equal(dst.Bytes(), append([]byte("prior:"), payload...)) {
				t.Fatalf("file bytes were interpreted as an API response: %v", err)
			}
		})
	}
}

func TestSDKProxyAndNativeRequestsKeepProactivePacing(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, noThrottle := range []bool{false, true} {
			name := "proxy"
			if native {
				name = "native"
			}
			if noThrottle {
				name += "/unpaced"
			} else {
				name += "/paced"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					calls := 0
					transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
						calls++
						return sdkRoutingResponse(r, `{"ok":true}`), nil
					})
					opts := api.SDKOptions{APIURL: "https://fixture.invalid/proxy/api", HTTPClient: &http.Client{Transport: transport}, NoThrottle: noThrottle}
					var nativeOpts []slackgo.Option
					if native {
						nativeOpts = []slackgo.Option{slackgo.OptionAPIURL("https://fixture.invalid/native/")}
					}
					client := api.NewSDKClient("xoxp-synthetic-routing-clock", opts, nativeOpts...)
					if err := client.SetUserCustomStatusContext(t.Context(), "First", ":memo:", 0); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
					defer cancel()
					start := time.Now()
					err := client.SetUserCustomStatusContext(ctx, "Second", ":memo:", 0)
					if noThrottle {
						if err != nil || calls != 2 || time.Since(start) != 0 {
							t.Fatal("no-throttle did not bypass proactive pacing")
						}
					} else if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != 100*time.Millisecond {
						t.Fatal("custom API prefix bypassed pacing or its context deadline")
					}
				})
			})
		}
	}
}

func TestSDKResponseGuardFollowsExplicitlyAllowedRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, method, healthy string
		call                  func(context.Context, *slackgo.Client) error
	}{
		{"status", "users.profile.set", `{"ok":true}`, func(ctx context.Context, c *slackgo.Client) error {
			return c.SetUserCustomStatusContext(ctx, "Review", ":memo:", 0)
		}},
		{"multi-user lookup", "users.info", `{"ok":true,"users":[{"id":"U_FIRST"},{"id":"U_SECOND"}]}`, func(ctx context.Context, c *slackgo.Client) error {
			users, err := c.GetUsersInfoContext(ctx, "U_FIRST", "U_SECOND")
			if err == nil && (users == nil || len(*users) != 2) {
				return errors.New("redirected lookup lost user results")
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests, checks := 0, 0
			transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests++
				if requests%2 == 1 {
					if r.Method != http.MethodPost || r.URL.Path != "/proxy/api/"+tc.method {
						t.Fatal("redirect lost its original API operation")
					}
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://fixture.invalid/opaque-result"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.Method != http.MethodGet || r.URL.Path != "/opaque-result" {
					t.Fatal("caller redirect policy was ignored")
				}
				body := "{}"
				if requests == 4 {
					body = tc.healthy
				}
				return sdkRoutingResponse(r, body), nil
			})
			supplied := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { checks++; return nil }}
			client := api.NewSDKClient("xoxp-synthetic-routing-redirect", api.SDKOptions{APIURL: "https://fixture.invalid/proxy/api", HTTPClient: supplied, NoThrottle: true})
			if err := tc.call(t.Context(), client); err == nil || requests != 2 || checks != 1 {
				t.Error("allowed redirect bypassed the original acknowledgement guard or replayed the operation")
			}
			if err := tc.call(t.Context(), client); err != nil || requests != 4 || checks != 2 {
				t.Fatalf("redirected request did not recover explicitly: %v", err)
			}
		})
	}
}

func sdkRoutingResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestSDKMultipartTransfersWithMethodNamesRemainOpaque(t *testing.T) {
	for _, name := range []string{"upload", "reactions.get", "reactions.add", "reactions.remove"} {
		t.Run(name, func(t *testing.T) {
			const token = "xoxb-synthetic-transfer-routing"
			const payload = "Review bundle\x00\xff"
			calls := 0
			transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/files-pri/T/F/"+name || r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatal("transfer changed its method, destination or authentication")
				}
				reader, err := r.MultipartReader()
				if err != nil {
					t.Fatal(err)
				}
				part, err := reader.NextPart()
				if err != nil || part.FormName() != "file" || part.FileName() != "bundle.bin" {
					t.Fatal("transfer lost its file part")
				}
				body, err := io.ReadAll(part)
				if err != nil || string(body) != payload {
					t.Fatal("transfer changed file bytes")
				}
				if _, err := reader.NextPart(); err != io.EOF {
					t.Fatal("transfer added unrelated form fields")
				}
				return sdkRoutingResponse(r, "transfer complete"), nil
			})
			client := api.NewSDKClient(token, api.SDKOptions{APIURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: transport}})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := client.UploadToURL(ctx, slackgo.UploadToURLParameters{UploadURL: "https://fixture.invalid/files-pri/T/F/" + name, Content: payload, Filename: "bundle.bin"}); err != nil || calls != 1 {
				t.Fatalf("transfer reply was interpreted as API JSON: %v", err)
			}
		})
	}
}

func TestSDKCallerHTTPTimeoutIncludesPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			return sdkRoutingResponse(r, `{"ok":true}`), nil
		})
		supplied := &http.Client{Transport: transport, Timeout: 50 * time.Millisecond}
		client := api.NewSDKClient("xoxp-synthetic-routing-timeout", api.SDKOptions{APIURL: "https://fixture.invalid/proxy/api", HTTPClient: supplied})
		if err := client.SetUserCustomStatusContext(t.Context(), "First", ":memo:", 0); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err := client.SetUserCustomStatusContext(t.Context(), "Second", ":memo:", 0)
		if !errors.Is(err, context.DeadlineExceeded) || t.Context().Err() != nil || calls != 1 || time.Since(start) != 50*time.Millisecond {
			t.Fatal("caller HTTP timeout did not bound the pacing wait")
		}
		if supplied.Timeout != 50*time.Millisecond {
			t.Fatal("factory changed the caller HTTP timeout")
		}
	})
}

func TestSDKRedirectHopsShareOnePacingReservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://fixture.invalid/result"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			}
			return sdkRoutingResponse(r, `{"ok":true}`), nil
		})
		supplied := &http.Client{Transport: transport, Timeout: 100 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
		client := api.NewSDKClient("xoxp-synthetic-routing-redirect-clock", api.SDKOptions{APIURL: "https://fixture.invalid/proxy/api", HTTPClient: supplied})
		start := time.Now()
		if err := client.SetUserCustomStatusContext(t.Context(), "Review", ":memo:", 0); err != nil || calls != 2 || time.Since(start) != 0 {
			t.Fatalf("redirect hop consumed another pacing reservation: %v", err)
		}
	})
}

func TestSDKNativeGETAndJSONCallsKeepPacing(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, tc := range []struct {
			name, method, verb, healthy string
			call                        func(context.Context, *slackgo.Client) error
		}{
			{"GET", "chat.getPermalink", http.MethodGet, `{"ok":true,"permalink":"https://fixture.invalid/message"}`, func(ctx context.Context, c *slackgo.Client) error {
				_, err := c.GetPermalinkContext(ctx, &slackgo.PermalinkParameters{Channel: "C_FIXTURE", Ts: "1746284582.000001"})
				return err
			}},
			{"JSON", "views.open", http.MethodPost, `{"ok":true,"view":{"id":"V_FIXTURE","type":"modal"}}`, func(ctx context.Context, c *slackgo.Client) error {
				_, err := c.OpenViewContext(ctx, "synthetic-trigger", slackgo.ModalViewRequest{})
				return err
			}},
		} {
			name := "configured/" + tc.name
			if native {
				name = "native/" + tc.name
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					const token = "xoxp-synthetic-routing-protocol"
					calls := 0
					prefix := "/proxy/api/"
					if native {
						prefix = "/native/"
					}
					transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.Method != tc.verb || r.URL.Path != prefix+tc.method || r.Header.Get("Authorization") != "Bearer "+token {
							t.Fatal("native SDK protocol changed method, endpoint or authentication")
						}
						return sdkRoutingResponse(r, tc.healthy), nil
					})
					opts := api.SDKOptions{APIURL: "https://fixture.invalid/proxy/api", HTTPClient: &http.Client{Transport: transport}}
					var nativeOpts []slackgo.Option
					if native {
						nativeOpts = []slackgo.Option{slackgo.OptionAPIURL("https://fixture.invalid/native/")}
					}
					client := api.NewSDKClient(token, opts, nativeOpts...)
					if err := tc.call(t.Context(), client); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
					defer cancel()
					start := time.Now()
					if err := tc.call(ctx, client); !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != 100*time.Millisecond {
						t.Fatal("native GET or JSON protocol bypassed proactive pacing")
					}
				})
			})
		}
	}
}

func TestSDKNativeHTTPClientOptionRetainsCallerOverride(t *testing.T) {
	fallbackCalls, nativeCalls := 0, 0
	fallback := &http.Client{Transport: sdkTestRoundTripper(func(*http.Request) (*http.Response, error) {
		fallbackCalls++
		return nil, errors.New("unexpected fallback transport")
	})}
	native := &http.Client{Transport: sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		nativeCalls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/users.profile.set" {
			t.Fatal("native override changed its request")
		}
		return sdkRoutingResponse(r, `{"ok":true}`), nil
	})}
	client := api.NewSDKClient("xoxp-synthetic-native-http", api.SDKOptions{APIURL: "https://fixture.invalid", HTTPClient: fallback}, slackgo.OptionHTTPClient(native))
	if err := client.SetUserCustomStatusContext(t.Context(), "Review", ":memo:", 0); err != nil || nativeCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("native HTTP-client option lost its precedence: %v", err)
	}
}

func TestSDKCanonicalMultipartRPCsKeepPacing(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "configured"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const token = "xoxe-synthetic-config-routing"
				calls := 0
				transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/apps.icon.set" || r.Header.Get("Authorization") != "Bearer "+token || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
						t.Fatal("multipart API call lost its native protocol")
					}
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Fatal(err)
					}
					return sdkRoutingResponse(r, `{"ok":true}`), nil
				})
				opts := api.SDKOptions{APIURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: transport}}
				var nativeOpts []slackgo.Option
				if native {
					opts.APIURL = ""
					nativeOpts = []slackgo.Option{slackgo.OptionAPIURL("https://fixture.invalid/api/")}
				}
				client := api.NewSDKClient("xoxp-synthetic-routing", opts, nativeOpts...)
				call := func(ctx context.Context) error {
					return client.SetAppIconContext(ctx, token, "A_FIXTURE", slackgo.SetAppIconParameters{File: strings.NewReader("icon-bytes"), Filename: "icon.png"})
				}
				if err := call(t.Context()); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				start := time.Now()
				if err := call(ctx); !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != 100*time.Millisecond {
					t.Fatal("canonical native multipart RPC lost its existing pacing")
				}
			})
		})
	}
}

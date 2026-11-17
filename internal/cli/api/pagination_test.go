package api_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCursorPaginationBoundsResumeAndEmptyResults(t *testing.T) {
	for _, maxPages := range []int{1, 3} {
		t.Run(fmt.Sprint(maxPages), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				n := requests.Add(1)
				cursor := req.URL.Query().Get("cursor")
				if n == 1 && cursor != "" || n == 2 && cursor != "cursor-2" {
					t.Errorf("wrong cursor: %q", cursor)
				}
				if n == 1 {
					_, _ = io.WriteString(w, `{"ok":true,"channels":[],"response_metadata":{"next_cursor":"cursor-2"}}`)
				} else {
					_, _ = io.WriteString(w, `{"ok":true,"channels":[],"response_metadata":{"next_cursor":""}}`)
				}
			}))
			defer server.Close()
			result, _, err := run(t, server.URL+"/", "", nil, "api", "call", "conversations.list", "--paginate", fmt.Sprintf("--max-pages=%d", maxPages))
			if err != nil || result["ok"] != true {
				t.Fatalf("pagination failed: %#v %v", result, err)
			}
			data := result["data"].(map[string]any)
			want := float64(2)
			if maxPages == 1 {
				want = 1
				if data["next_cursor"] != "cursor-2" || data["complete"] != false {
					t.Fatal("bounded result lost resume cursor")
				}
			} else if data["complete"] != true {
				t.Fatal("did not complete")
			}
			if data["completed_pages"] != want || float64(requests.Load()) != want || len(data["pages"].([]any)) != int(want) {
				t.Fatalf("wrong page counts: %#v", data)
			}
		})
	}
}

func TestPagePaginationUsesNativeBoundsAndPreservesInitialPage(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := requests.Add(1)
		want := fmt.Sprint(n + 1)
		if req.URL.Query().Get("page") != want || req.URL.Query().Get("count") != "2" {
			t.Errorf("wrong page arguments: %v", req.URL.Query())
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"files":[],"paging":{"page":%d,"count":2,"total":6}}`, n+1)
	}))
	defer server.Close()
	result, _, err := run(t, server.URL+"/", "", nil, "api", "call", "files.list", "--paginate", "--field", "page=2", "--field", "count=2")
	if err != nil || result["ok"] != true || requests.Load() != 2 {
		t.Fatalf("page pagination failed: %#v %v requests=%d", result, err, requests.Load())
	}
	data := result["data"].(map[string]any)
	if data["complete"] != true || data["next_page"] != nil {
		t.Fatalf("incorrect final page: %#v", data)
	}
}

func TestCursorInitialAndTruncatedResponseValidation(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if truncated {
					_, _ = io.WriteString(w, `{"ok":true,"channels":[],"has_more":true}`)
					return
				}
				if req.URL.Query().Get("cursor") != "*" {
					t.Errorf("search missing documented initialcursor: %v", req.URL.Query())
				}
				_, _ = io.WriteString(w, `{"ok":true,"messages":{"matches":[],"pagination":{"next_cursor":""}}}`)
			}))
			defer server.Close()
			method := "search.messages"
			args := []string{"--field", "query=fixture"}
			if truncated {
				method = "conversations.list"
				args = nil
			}
			result, _, err := run(t, server.URL+"/", "", nil, append([]string{"api", "call", method, "--paginate"}, args...)...)
			if truncated {
				if err == nil || result["ok"] != false {
					t.Fatal("truncated pagination reported complete")
				}
				return
			}
			if err != nil || result["ok"] != true {
				t.Fatalf("initial cursor failed: %#v %v", result, err)
			}
		})
	}
}

func TestPaginationFailureAndRepeatedCursorNeverReportPartialSuccess(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(fmt.Sprint(repeat), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := requests.Add(1)
				if n == 1 || repeat {
					_, _ = io.WriteString(w, `{"ok":true,"channels":[],"response_metadata":{"next_cursor":"cursor-2"}}`)
				} else {
					_, _ = io.WriteString(w, `{"ok":false,"error":"no_permission"}`)
				}
			}))
			defer server.Close()
			result, _, err := run(t, server.URL+"/", "", nil, "api", "call", "conversations.list", "--paginate")
			if err == nil || result["ok"] != false || result["data"] != nil || requests.Load() != 2 {
				t.Fatalf("partial success or replay: %#v %v requests=%d", result, err, requests.Load())
			}
			details := result["errors"].([]any)[0].(map[string]any)["details"].(map[string]any)
			if details["next_cursor"] != "cursor-2" || details["completed_pages"] == nil {
				t.Fatalf("missing resume details: %#v", result)
			}
		})
	}
}

func TestInputFailuresDoNotCallSlackOrExposeCredentials(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `{`, `{} {}`, `{"token":"fixture-secret"}`, `{"channel":"C1","metadata":{"authorization":"fixture-secret"}}`, `{"channel":"C1","metadata":{"apiKey":"fixture-secret"}}`, `{}` + strings.Repeat(" ", 4<<20)} {
		result, stderr, err := run(t, "", input, noCredentials(t), "api", "call", "chat.postMessage", "--input=-", "--dry-run")
		if err == nil || result["ok"] != false || strings.Contains(fmt.Sprint(result)+stderr+err.Error(), "fixture-secret") {
			t.Fatalf("unsafe input result: %#v %s %v", result, stderr, err)
		}
	}
	for _, field := range []string{"token=fixture-secret", "=value", "missing-equals"} {
		result, stderr, err := run(t, "", "", noCredentials(t), "api", "call", "chat.postMessage", "--dry-run", "--field", field)
		if err == nil || result["ok"] != false || strings.Contains(fmt.Sprint(result)+stderr+err.Error(), "fixture-secret") {
			t.Fatal("unsafe field")
		}
	}
}

func TestMalformedPaginationNeverClaimsCompletion(t *testing.T) {
	for _, fixture := range []struct{ method, body string }{
		{"conversations.list", `{"ok":true,"response_metadata":{"next_cursor":123}}`},
		{"conversations.list", `{"ok":true,"response_metadata":[]}`},
		{"conversations.list", `{"ok":true,"has_more":true}`},
		{"files.list", `{"ok":true,"paging":{"page":1}}`},
		{"files.list", `{"ok":true,"paging":{"page":1,"pages":1.5}}`},
		{"files.list", `{"ok":true,"paging":{"page":1.5,"pages":3}}`},
		{"files.list", `{"ok":true,"paging":{"page":1,"pages":1e100}}`},
		{"files.list", `{"ok":true,"paging":{"page":3,"pages":2}}`},
		{"files.list", `{"ok":true,"paging":{"page":1,"count":0,"total":10}}`},
	} {
		t.Run(fixture.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, fixture.body) }))
			defer server.Close()
			result, _, err := run(t, server.URL+"/", "", nil, "api", "call", fixture.method, "--paginate")
			if err == nil || result["ok"] != false || result["data"] != nil {
				t.Fatalf("malformed pagination reported success: %#v %v", result, err)
			}
		})
	}
}

func TestFailedRequestRemovesOnlyItsNewOutputFileAndRecovers(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"ok":false,"error":"no_permission"}`)
		} else {
			_, _ = io.WriteString(w, `{"ok":true,"user":{"id":"U1"}}`)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "output.json")
	args := []string{"api", "call", "users.info", "--field", "user=U1", "--output-file", path}
	result, _, err := run(t, server.URL+"/", "", nil, args...)
	if err == nil || result["ok"] != false {
		t.Fatal("expected permission failure")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed response left output: %v", err)
	}
	result, _, err = run(t, server.URL+"/", "", nil, args...)
	if err != nil || result["ok"] != true || requests.Load() != 2 {
		t.Fatalf("recovery failed: %#v %v", result, err)
	}
}

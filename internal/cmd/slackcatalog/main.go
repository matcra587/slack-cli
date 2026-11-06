// Command slackcatalog refreshes request metadata from Slack's official Markdown
// method index and records the public client surface of the pinned Go SDK.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/matcra587/slack-cli/internal/slackcatalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	indexURL := flag.String("index", "https://docs.slack.dev/llms-sitemap.md", "official method sitemap URL or local snapshot path")
	docsDir := flag.String("docs-dir", "", "read method Markdown from an existing local snapshot directory")
	sdkDir := flag.String("sdk-dir", "", "source directory of the pinned slack-go/slack module")
	sdkVersion := flag.String("sdk-version", "v0.35.0", "version corresponding to sdk-dir")
	date := flag.String("date", time.Now().UTC().Format(time.DateOnly), "source retrieval date (YYYY-MM-DD)")
	output := flag.String("output", "internal/slackcatalog/methods.json", "output catalog")
	coveragePath := flag.String("coverage", "internal/slackcatalog/sdk.json", "output SDK coverage inventory")
	flag.Parse()
	if *sdkDir == "" {
		return errors.New("-sdk-dir is required; use go list -m -f '{{.Dir}}' github.com/slack-go/slack")
	}
	if _, err := time.Parse(time.DateOnly, *date); err != nil {
		return fmt.Errorf("invalid retrieval date: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.Host != "docs.slack.dev" || len(via) >= 5 {
				return errors.New("documentation redirect is outside the trusted origin or exceeds the redirect limit")
			}
			return nil
		},
	}
	index, err := readIndex(ctx, client, *indexURL)
	if err != nil {
		return err
	}
	urls := methodURLs(string(index))
	if len(urls) == 0 {
		return errors.New("method sitemap contains no official method pages")
	}
	methods, err := loadMethods(ctx, client, urls, *docsDir)
	if err != nil {
		return err
	}
	coverage, err := scanSDK(*sdkDir, *sdkVersion)
	if err != nil {
		return err
	}
	methods = joinSDK(methods, coverage)
	snapshot := slackcatalog.Snapshot{
		GeneratedAt: *date, IndexURL: "https://docs.slack.dev/llms-sitemap.md", IndexHash: hash(index),
		SDKVersion: *sdkVersion, OfficialMethods: len(urls), SDKClientMethods: len(coverage.Methods), Methods: methods,
	}
	return publishJSON([]jsonOutput{{*output, snapshot}, {*coveragePath, coverage}}, os.Rename)
}

func loadMethods(ctx context.Context, client *http.Client, urls []string, directory string) ([]slackcatalog.Method, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	methods := make([]slackcatalog.Method, len(urls))
	jobs := make(chan int)
	errorsFound := make(chan error, 1)
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			for index := range jobs {
				source := urls[index]
				var raw []byte
				var err error
				if directory != "" {
					raw, err = os.ReadFile(filepath.Join(directory, filepath.Base(source))) //nolint:gosec // A user-selected local snapshot is an explicit generator input.
				} else {
					raw, err = fetch(ctx, client, source)
				}
				var method slackcatalog.Method
				if err == nil {
					method, err = parseMethod(raw)
				}
				if err != nil {
					select {
					case errorsFound <- fmt.Errorf("reading method %s: %w", source, err):
						cancel()
					default:
					}
					return
				}
				methods[index] = method
			}
		})
	}
	for index := range urls {
		select {
		case jobs <- index:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	select {
	case err := <-errorsFound:
		return nil, err
	default:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return methods, nil
	}
}

func readIndex(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") {
		return fetch(ctx, client, source)
	}
	return os.ReadFile(source) //nolint:gosec // The generator explicitly accepts a user-selected local documentation snapshot.
}

func fetch(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host != "docs.slack.dev" {
		return nil, errors.New("source must be an HTTPS URL on docs.slack.dev")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	const maxSourceBytes = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSourceBytes {
		return nil, errors.New("documentation source exceeds the size limit")
	}
	return raw, nil
}

func methodURLs(index string) []string {
	pattern := regexp.MustCompile(`https://docs\.slack\.dev/reference/methods/[a-zA-Z0-9.]+\.md`)
	urls := pattern.FindAllString(index, -1)
	sort.Strings(urls)
	return slices.Compact(urls)
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

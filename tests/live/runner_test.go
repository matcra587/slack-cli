package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const liveWorkspace = "live-oauth-isolated"

var (
	errLiveDisabled = errors.New("live confirmation is absent")
	errLiveSafety   = errors.New("live safety check failed")
	slackID         = regexp.MustCompile(`^[A-Z][A-Z0-9]+$`)
	slackTS         = regexp.MustCompile(`^[0-9]+\.[0-9]{6}$`)
)

type livePlan struct {
	RunRoot        string   `json:"run_root"`
	ConfigPath     string   `json:"config_path"`
	Workspace      string   `json:"workspace"`
	TeamID         string   `json:"team_id"`
	ActorID        string   `json:"actor_id"`
	PublicChannel  string   `json:"public_channel_id"`
	PrivateChannel string   `json:"private_channel_id"`
	Recipients     []string `json:"recipient_ids"`
	RunID          string   `json:"run_id"`
	Binary         string   `json:"-"`
}

// The explicit confirmation and plan path are checked before credentials or
// ordinary configuration can be consulted. Diagnostics deliberately omit input.
func loadLivePlan(getenv func(string) string) (livePlan, error) {
	if getenv("SLICK_LIVE_CONFIRM") != "designated-fixtures" {
		return livePlan{}, errLiveDisabled
	}
	path := getenv("SLICK_LIVE_PLAN")
	if !filepath.IsAbs(path) {
		return livePlan{}, errLiveSafety
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 16<<10 {
		return livePlan{}, errLiveSafety
	}
	f, err := os.Open(path)
	if err != nil {
		return livePlan{}, errLiveSafety
	}
	defer func() { _ = f.Close() }()
	p, err := decodeLivePlan(io.LimitReader(f, 16385))
	if err != nil {
		return livePlan{}, errLiveSafety
	}
	p.Binary = getenv("SLICK_LIVE_BINARY")
	if err := validateLivePlan(p); err != nil {
		return livePlan{}, err
	}
	return p, nil
}

// Token decoding detects duplicate keys (including escaped spellings) before
// unmarshalling; a map or struct decoder alone would silently accept them.
func decodeLivePlan(reader io.Reader) (livePlan, error) {
	required := map[string]bool{"run_root": true, "config_path": true, "workspace": true, "team_id": true, "actor_id": true, "public_channel_id": true, "private_channel_id": true, "recipient_ids": true, "run_id": true}
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return livePlan{}, errLiveSafety
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !required[key] {
			return livePlan{}, errLiveSafety
		}
		if _, exists := fields[key]; exists {
			return livePlan{}, errLiveSafety
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return livePlan{}, errLiveSafety
		}
		if key == "recipient_ids" {
			var ids []string
			if json.Unmarshal(value, &ids) != nil || ids == nil {
				return livePlan{}, errLiveSafety
			}
		} else {
			var text string
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &text) != nil {
				return livePlan{}, errLiveSafety
			}
		}
		fields[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || len(fields) != 9 || decoder.Decode(new(any)) != io.EOF {
		return livePlan{}, errLiveSafety
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return livePlan{}, errLiveSafety
	}
	var p livePlan
	if json.Unmarshal(data, &p) != nil {
		return livePlan{}, errLiveSafety
	}
	return p, nil
}

func validateLivePlan(p livePlan) error {
	if p.Workspace != liveWorkspace || !validID(p.TeamID, "T") || !validID(p.ActorID, "UW") ||
		!validID(p.PublicChannel, "C") || (p.PrivateChannel != "" && !validID(p.PrivateChannel, "CG")) ||
		p.PrivateChannel == p.PublicChannel ||
		!regexp.MustCompile(`^[a-zA-Z0-9-]{8,64}$`).MatchString(p.RunID) || !filepath.IsAbs(p.RunRoot) {
		return errLiveSafety
	}
	root, err := filepath.EvalSymlinks(p.RunRoot)
	if err != nil || root != filepath.Clean(p.RunRoot) {
		return errLiveSafety
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errLiveSafety
	}
	for _, path := range []string{p.ConfigPath, p.Binary} {
		actual, err := filepath.EvalSymlinks(path)
		rel, relErr := filepath.Rel(root, actual)
		if err != nil || relErr != nil || !filepath.IsAbs(path) || actual != filepath.Clean(path) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errLiveSafety
		}
		info, err := os.Stat(actual)
		if err != nil || !info.Mode().IsRegular() {
			return errLiveSafety
		}
	}
	configInfo, _ := os.Stat(p.ConfigPath)
	if configInfo.Mode().Perm()&0o077 != 0 || configInfo.Size() > 16<<10 {
		return errLiveSafety
	}
	// A tiny strict config prevents references to everyday credential providers,
	// OAuth refresh state, aliases and additional profiles.
	var cfg struct {
		Schema     string `toml:"schema_version"`
		Default    string `toml:"default_workspace"`
		Workspaces map[string]struct {
			Name string `toml:"name"`
			Team string `toml:"team_id"`
			Type string `toml:"token_type"`
			Ref  string `toml:"token_ref"`
		} `toml:"workspaces"`
	}
	meta, err := toml.DecodeFile(p.ConfigPath, &cfg)
	profile, ok := cfg.Workspaces[liveWorkspace]
	if err != nil || len(meta.Undecoded()) != 0 || cfg.Schema != "1" || cfg.Default != liveWorkspace || len(cfg.Workspaces) != 1 || !ok ||
		profile.Name != liveWorkspace || profile.Team != p.TeamID || (profile.Type != "user" && profile.Type != "bot") || profile.Ref != "env:SLICK_LIVE_ACCESS_TOKEN" {
		return errLiveSafety
	}
	seen := map[string]bool{}
	for _, id := range p.Recipients {
		if !validID(id, "UW") || seen[id] || id == p.ActorID {
			return errLiveSafety
		}
		seen[id] = true
	}
	return nil
}

func validID(id, prefixes string) bool {
	return slackID.MatchString(id) && strings.ContainsRune(prefixes, rune(id[0]))
}

type liveReceipt struct {
	Kind, Channel, TS, ParentTS, ScheduledID, FileID, Emoji string
	Marker, Oldest, Latest                                  string
	PostFrom, PostTo                                        int64
}
type liveObserver interface {
	Identity(context.Context) (string, string, error)
	Channel(context.Context, string) error
	Inspect(context.Context, liveReceipt) (bool, error)
	Delete(context.Context, liveReceipt) error
	Conversation(context.Context, []string) (string, error)
	Recover(context.Context, liveReceipt) ([]liveReceipt, error)
}
type (
	liveExec   func(context.Context, string, []string, []string, string) ([]byte, error)
	liveRunner struct {
		plan          livePlan
		env           []string
		observer      liveObserver
		exec          liveExec
		ctx           context.Context //nolint:containedctx // This test runner owns and cancels its bounded lifecycle context.
		stop          context.CancelFunc
		receipts      []liveReceipt
		ambiguous     bool
		ready         bool
		allowFiles    bool
		prior         []byte
		ledger        string
		conversations map[string]string
		sequence      int
		finished      map[string]bool
		onCommand     func(string, int, string, string)
	}
)

func newLiveRunner(p livePlan, token string, observer liveObserver, execute liveExec) (*liveRunner, error) {
	if err := validateLivePlan(p); err != nil {
		return nil, err
	}
	if token == "" || strings.TrimSpace(token) != token {
		return nil, errLiveSafety
	}
	prior, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		return nil, errLiveSafety
	}
	home, err := os.MkdirTemp(p.RunRoot, "runtime-")
	if err != nil {
		return nil, errLiveSafety
	}
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Minute)
	env := []string{"HOME=" + home, "USERPROFILE=" + home, "XDG_CONFIG_HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "TMPDIR=" + home, "TMP=" + home, "TEMP=" + home, "SLICK_CONFIG=" + p.ConfigPath, "SLACK_CLI_CONFIG=" + p.ConfigPath, "SLICK_LIVE_ACCESS_TOKEN=" + token}
	for _, key := range []string{"SystemRoot", "WINDIR"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	ledger := filepath.Join(p.RunRoot, "receipts-"+p.RunID+".jsonl")
	f, err := os.OpenFile(ledger, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		stop()
		return nil, errLiveSafety
	}
	if f.Close() != nil {
		stop()
		return nil, errLiveSafety
	}
	return &liveRunner{plan: p, env: env, observer: observer, exec: execute, ctx: ctx, stop: stop, prior: prior, ledger: ledger, conversations: map[string]string{}, finished: map[string]bool{}}, nil
}

func (r *liveRunner) preflight() error {
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	team, actor, err := r.observer.Identity(ctx)
	if err != nil || team != r.plan.TeamID || actor != r.plan.ActorID {
		return errLiveSafety
	}
	for _, channel := range []string{r.plan.PublicChannel, r.plan.PrivateChannel} {
		if channel != "" {
			if r.observer.Channel(ctx, channel) != nil {
				return errLiveSafety
			}
		}
	}
	r.ready = true
	return nil
}

func (r *liveRunner) allowedChannel(ch string) bool {
	return ch != "" && (ch == r.plan.PublicChannel || ch == r.plan.PrivateChannel || r.conversations[ch] != "")
}

func (r *liveRunner) admitRecipients(ids []string) (string, error) {
	if !r.ready || r.ambiguous || r.ctx.Err() != nil || len(ids) < 1 || len(ids) > 2 {
		return "", errLiveSafety
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !slices.Contains(r.plan.Recipients, id) || seen[id] {
			return "", errLiveSafety
		}
		seen[id] = true
	}
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	channel, err := r.observer.Conversation(ctx, ids)
	if err != nil || !validID(channel, "CDG") {
		return "", errLiveSafety
	}
	r.conversations[channel] = strings.Join(ids, ",")
	return channel, nil
}

func (r *liveRunner) owns(ch, ts string) bool {
	return slices.ContainsFunc(r.receipts, func(v liveReceipt) bool { return v.Kind == "message" && v.Channel == ch && v.TS == ts })
}

// All mutations use machine output so exact receipts can be registered before
// callers assert content. Receipt extraction also runs on a nonzero exit.
func (r *liveRunner) command(op, ch, target, body string) ([]byte, error) {
	if !r.ready || r.ambiguous || r.ctx.Err() != nil || !r.allowedChannel(ch) {
		return nil, errLiveSafety
	}
	var args []string
	creation := op == "send" || strings.HasPrefix(op, "send-users") || op == "reply" || op == "schedule" || op == "schedule-users" || op == "upload"
	started := time.Now()
	pending := liveReceipt{Kind: "uncertain-" + op, Channel: ch, Oldest: fmt.Sprintf("%d.000000", started.Add(-5*time.Second).Unix())}
	if creation {
		if !strings.Contains(body, r.plan.RunID) {
			return nil, errLiveSafety
		}
		r.sequence++
		pending.Marker = fmt.Sprintf("%s-created-%d", r.plan.RunID, r.sequence)
		body += "\n\n[" + pending.Marker + "]"
		if op == "reply" {
			pending.ParentTS = target
		}
		pending.PostFrom = started.Add(90*time.Minute - 5*time.Second).Unix()
	}
	switch op {
	case "send":
		args = []string{"message", "send", "--message", body}
	case "send-users", "send-users-repeated", "schedule-users":
		if r.conversations[ch] == "" || target != r.conversations[ch] {
			return nil, errLiveSafety
		}
		args = []string{"message", "send", "--message", body}
		if op == "schedule-users" {
			args = append(args, "--schedule", "90m")
		}
		if op == "send-users-repeated" {
			for id := range strings.SplitSeq(target, ",") {
				args = append(args, "--user", id)
			}
		} else {
			args = append(args, "--user", target)
		}
	case "reply":
		if !r.owns(ch, target) {
			return nil, errLiveSafety
		}
		args = []string{"reply", "--parent", target, "--message", body}
	case "edit":
		if !r.owns(ch, target) {
			return nil, errLiveSafety
		}
		args = []string{"message", "edit", "--timestamp", target, "--message", body}
	case "delete":
		if !r.owns(ch, target) {
			return nil, errLiveSafety
		}
		args = []string{"message", "delete", "--timestamp", target, "--force"}
	case "add", "remove":
		if !r.owns(ch, target) {
			return nil, errLiveSafety
		}
		args = []string{"react", op, "--timestamp", target, "--emoji", "eyes"}
	case "watch":
		if !r.owns(ch, target) {
			return nil, errLiveSafety
		}
		args = []string{"react", "watch", "--timestamp", target, "--emoji", "eyes", "--user", r.plan.ActorID, "--interval", "1s"}
	case "schedule":
		args = []string{"message", "send", "--message", body, "--schedule", "90m"}
	case "cancel":
		if !slices.ContainsFunc(r.receipts, func(v liveReceipt) bool { return v.Kind == "scheduled" && v.Channel == ch && v.ScheduledID == target }) {
			return nil, errLiveSafety
		}
		args = []string{"message", "scheduled", "delete", "--scheduled-id", target}
	case "history":
		args = []string{"history", "list", "--max-items", "20"}
	case "upload":
		if !r.allowFiles {
			return nil, errLiveSafety
		}
		actual, err := filepath.EvalSymlinks(target)
		rel, relErr := filepath.Rel(r.plan.RunRoot, actual)
		if err != nil || relErr != nil || strings.HasPrefix(rel, "..") || actual != target {
			return nil, errLiveSafety
		}
		args = []string{"file", "upload", "--file", target, "--title", body, "--no-attribution"}
	default:
		return nil, errLiveSafety
	}
	if (op == "send" || strings.HasPrefix(op, "send-users") || op == "reply" || op == "edit" || op == "schedule" || op == "schedule-users" || op == "upload") && !strings.Contains(body, r.plan.RunID) {
		return nil, errLiveSafety
	}
	args = append(args, "--workspace", r.plan.Workspace, "--output=json", "--color=never", "--timeout=10s")
	if !strings.HasPrefix(op, "send-users") && op != "schedule-users" {
		args = append(args, "--channel", ch)
	}
	if op == "send" || strings.HasPrefix(op, "send-users") || op == "reply" || op == "edit" || op == "schedule" || op == "schedule-users" {
		args = append(args, "--no-attribution")
	}
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer cancel()
	out, err := r.execute(ctx, args, r.env, "")
	if op == "add" {
		if !r.record(liveReceipt{Kind: "reaction", Channel: ch, TS: target, Emoji: "eyes"}) {
			r.ambiguous = true
			return nil, errLiveSafety
		}
	}
	if creation {
		if !r.track(op, ch, target, out) {
			r.ambiguous = true
			pending.Latest = fmt.Sprintf("%d.999999", time.Now().Add(5*time.Second).Unix())
			pending.PostTo = time.Now().Add(90*time.Minute + 5*time.Second).Unix()
			_ = r.record(pending)
			return nil, errors.New("live mutation outcome uncertain; inspect exact run before retry")
		}
	}
	if err != nil {
		return nil, errors.New("live command failed; diagnostics withheld")
	}
	return out, nil
}

func (r *liveRunner) track(op, ch, parent string, out []byte) bool {
	if op != "reply" {
		parent = ""
	}
	var frame struct {
		Errors []struct {
			Details struct {
				FileID string `json:"file_id"`
				Stage  string
			}
		}
		Data struct {
			Message struct{ Channel, TS string }
			File    struct{ ID string }
			Channel string
			ID      string `json:"scheduled_message_id"`
		}
	}
	if json.Unmarshal(out, &frame) != nil {
		return false
	}
	if op == "upload" {
		if frame.Data.File.ID == "" {
			for _, failure := range frame.Errors {
				if failure.Details.Stage == "transfer" || failure.Details.Stage == "complete" {
					frame.Data.File.ID = failure.Details.FileID
					break
				}
			}
		}
		if !validID(frame.Data.File.ID, "F") {
			return false
		}
		return r.record(liveReceipt{Kind: "file", Channel: ch, FileID: frame.Data.File.ID})
	}
	v := liveReceipt{Kind: "message", Channel: frame.Data.Message.Channel, TS: frame.Data.Message.TS, ParentTS: parent}
	if op == "schedule" || op == "schedule-users" {
		v = liveReceipt{Kind: "scheduled", Channel: frame.Data.Channel, ScheduledID: frame.Data.ID}
		if !validID(v.ScheduledID, "Q") {
			return false
		}
	} else if !slackTS.MatchString(v.TS) {
		return false
	}
	if v.Channel != ch {
		return false
	}
	return r.record(v)
}

func (r *liveRunner) record(v liveReceipt) bool {
	r.receipts = append(r.receipts, v)
	return r.writeLedger(v)
}

func (r *liveRunner) writeLedger(v liveReceipt) bool {
	f, err := os.OpenFile(r.ledger, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false
	}
	err = json.NewEncoder(f).Encode(v)
	syncErr, closeErr := f.Sync(), f.Close()
	return err == nil && syncErr == nil && closeErr == nil
}

func (r *liveRunner) cleanup() error {
	r.stop()
	// Independent context survives cancellation of the normal phase.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	failures := 0
	for i, v := range slices.Backward(r.receipts) {
		if r.finished[receiptKey(v)] {
			continue
		}
		if strings.HasPrefix(v.Kind, "uncertain-") {
			call, done := context.WithTimeout(ctx, 20*time.Second)
			found, err := r.observer.Recover(call, v)
			done()
			if err != nil || len(found) != 1 || found[0].Channel != v.Channel || !r.allowedChannel(found[0].Channel) {
				failures++
				continue
			}
			v = found[0]
			v.Marker = r.receipts[i].Marker
			if (v.Kind != "message" && v.Kind != "scheduled") || (v.Kind == "message" && !slackTS.MatchString(v.TS)) || (v.Kind == "scheduled" && !validID(v.ScheduledID, "Q")) {
				failures++
				continue
			}
			if !r.record(v) {
				failures++
			}
		}
		call, done := context.WithTimeout(ctx, 10*time.Second)
		present, err := r.observer.Inspect(call, v)
		done()
		if err != nil {
			failures++
			continue
		}
		if !present {
			if r.markAbsent(v) != nil {
				failures++
			}
			continue
		}
		call, done = context.WithTimeout(ctx, 10*time.Second)
		err = r.observer.Delete(call, v)
		done()
		if err != nil {
			failures++
		}
		call, done = context.WithTimeout(ctx, 10*time.Second)
		present, err = r.observer.Inspect(call, v)
		done()
		if err != nil || present {
			failures++
		} else {
			if r.markAbsent(v) != nil {
				failures++
			}
		}
	}
	after, err := os.ReadFile(r.plan.ConfigPath)
	if err != nil || !bytes.Equal(after, r.prior) {
		failures++
	}
	if failures > 0 {
		return fmt.Errorf("live cleanup incomplete (%d checks); inspect private run ledger", failures)
	}
	return nil
}

func executeLiveChild(ctx context.Context, binary string, args, env []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	cmd.WaitDelay = 2 * time.Second
	var out liveOutputBuffer
	cmd.Stdout = &out
	// Raw remote diagnostics must never enter test logs.
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.Bytes(), err
}

type liveOutputBuffer struct{ buffer bytes.Buffer }

func (b *liveOutputBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *liveOutputBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 8<<20 {
		return 0, errors.New("live output exceeded private capture limit")
	}
	return b.buffer.Write(p)
}

func receiptKey(v liveReceipt) string {
	return strings.Join([]string{v.Kind, v.Channel, v.TS, v.ParentTS, v.ScheduledID, v.FileID, v.Emoji}, "|")
}

func (r *liveRunner) markAbsent(v liveReceipt) error {
	r.finished[receiptKey(v)] = true
	v.Kind = "absent-" + v.Kind
	if !r.writeLedger(v) {
		return errLiveSafety
	}
	return nil
}

func liveCLIPath(args []string) string {
	if len(args) < 2 {
		return "unknown"
	}
	path := args[0] + "." + args[1]
	if path == "message.scheduled" && len(args) > 2 {
		if args[2] == "list" || args[2] == "delete" {
			return path + "." + args[2]
		}
		return "unknown"
	}
	switch path {
	case "message.send", "message.edit", "message.delete", "react.add", "react.remove", "react.watch", "react.list", "lookup.channel", "lookup.user", "lookup.messages", "history.list", "cache.users", "cache.channels", "cache.clear", "auth.status", "health.current", "health.history", "health.api-test", "health.check", "file.upload":
		return path
	}
	if args[0] == "reply" {
		return "reply"
	}
	return "unknown"
}

// Only owned command names, numeric status and reviewed error codes enter logs.
func (r *liveRunner) execute(ctx context.Context, args, env []string, stdin string) ([]byte, error) {
	out, err := r.exec(ctx, r.plan.Binary, args, env, stdin)
	if r.onCommand != nil {
		exit := 0
		if err != nil {
			exit = -1
			var status interface{ ExitCode() int }
			if errors.As(err, &status) {
				exit = status.ExitCode()
			}
		}
		var frame struct {
			Errors []struct{ Type, Message string }
		}
		_ = json.Unmarshal(out, &frame)
		kind, code := "", ""
		if len(frame.Errors) > 0 {
			switch frame.Errors[0].Type {
			case "validation_error", "auth_failure", "not_found", "rate_limit", "server_error", "canceled", "timeout", "output_write_failed":
				kind = frame.Errors[0].Type
			}
			messageCode, _, _ := strings.Cut(frame.Errors[0].Message, ":")
			switch messageCode {
			case "missing_scope", "not_in_channel", "no_permission", "invalid_auth", "token_revoked", "channel_not_found", "message_not_found", "ratelimited":
				code = messageCode
			}
		}
		r.onCommand(liveCLIPath(args), exit, kind, code)
	}
	return out, err
}

func decodeLiveResult(out []byte, command, workspace string) (json.RawMessage, error) {
	var keys map[string]json.RawMessage
	var frame struct {
		OK     bool
		Data   json.RawMessage
		Errors []json.RawMessage
		Meta   struct {
			Command, Workspace string
			Exit               int `json:"exit_code"`
		}
	}
	if json.Unmarshal(out, &keys) != nil || len(keys) != 4 || json.Unmarshal(out, &frame) != nil || !frame.OK || frame.Errors == nil || len(frame.Errors) != 0 || frame.Meta.Command != command || frame.Meta.Workspace != workspace || frame.Meta.Exit != 0 || len(frame.Data) == 0 || string(frame.Data) == "null" {
		return nil, errLiveSafety
	}
	return frame.Data, nil
}

// The closed read surface never accepts an executable command from the plan.
func (r *liveRunner) read(ctx context.Context, args ...string) (json.RawMessage, error) {
	if !r.ready || r.ctx.Err() != nil {
		return nil, errLiveSafety
	}
	path := liveCLIPath(args)
	switch path {
	case "react.list", "message.scheduled.list", "lookup.channel", "lookup.user", "lookup.messages", "history.list", "cache.users", "cache.channels", "cache.clear", "health.current", "health.history", "health.api-test", "health.check":
	default:
		return nil, errLiveSafety
	}
	for i, arg := range args {
		if arg == "--channel" && (i+1 >= len(args) || !r.allowedChannel(args[i+1])) {
			return nil, errLiveSafety
		}
		if arg == "--user" && (i+1 >= len(args) || (args[i+1] != r.plan.ActorID && !slices.Contains(r.plan.Recipients, args[i+1]))) {
			return nil, errLiveSafety
		}
	}
	budget := 10 * time.Second
	if path == "cache.users" || path == "cache.channels" {
		// One-item pages deliberately exercise real client pacing across lists.
		budget = 75 * time.Second
	}
	args = append(slices.Clone(args), "--workspace", r.plan.Workspace, "--output=json", "--color=never", "--timeout="+budget.String())
	call, cancel := context.WithTimeout(ctx, budget+10*time.Second)
	defer cancel()
	out, err := r.execute(call, args, r.env, "")
	if err != nil {
		return nil, errors.New("live read failed; diagnostics withheld")
	}
	workspace := r.plan.Workspace
	switch path {
	case "health.current", "health.history", "health.api-test", "health.check":
		workspace = "health"
	}
	return decodeLiveResult(out, path, workspace)
}

type livePaging struct {
	Next string `json:"next_cursor"`
	More bool   `json:"has_more"`
}

func nextLivePage(p livePaging, seen map[string]bool) (bool, error) {
	if !p.More && p.Next == "" {
		return false, nil
	}
	if p.Next == "" || seen[p.Next] {
		return false, errLiveSafety
	}
	seen[p.Next] = true
	return true, nil
}

// threadReplies proves pagination, not merely that a small thread can be read.
func (r *liveRunner) threadReplies(ctx context.Context, parent liveReceipt, replies []liveReceipt) error {
	if !r.owns(parent.Channel, parent.TS) || len(replies) < 2 {
		return errLiveSafety
	}
	expected := make(map[string]bool, len(replies))
	for _, reply := range replies {
		if reply.Channel != parent.Channel || reply.ParentTS != parent.TS || !r.owns(reply.Channel, reply.TS) {
			return errLiveSafety
		}
		if _, exists := expected[reply.TS]; exists {
			return errLiveSafety
		}
		expected[reply.TS] = false
	}
	cursor := ""
	seen := map[string]bool{"": true}
	pages := 0
	for {
		args := []string{"history", "list", "--channel", parent.Channel, "--thread", parent.TS, "--max-items", "1"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		data, err := r.read(ctx, args...)
		if err != nil {
			return err
		}
		var page struct {
			Messages []struct {
				TS     string
				Thread string `json:"thread_ts"`
			}
			Paging *livePaging `json:"pagination"`
		}
		if json.Unmarshal(data, &page) != nil || page.Messages == nil || page.Paging == nil {
			return errors.New("CLI thread page lost messages or pagination")
		}
		pages++
		for _, message := range page.Messages {
			already, owned := expected[message.TS]
			if !owned {
				continue
			}
			if already || message.Thread != parent.TS {
				return errors.New("CLI thread pagination duplicated a reply or changed its parent")
			}
			expected[message.TS] = true
		}
		more, err := nextLivePage(*page.Paging, seen)
		if err != nil {
			return errors.New("CLI thread continuation is missing or repeated")
		}
		if !more {
			break
		}
		if pages > len(replies)+2 {
			return errors.New("CLI thread pagination exceeded fixture page budget")
		}
		cursor = page.Paging.Next
	}
	if pages < 2 {
		return errors.New("CLI thread pagination was not exercised: Slack returned no continuation for the larger fixture")
	}
	for _, found := range expected {
		if !found {
			return errors.New("CLI thread pagination concealed an owned reply")
		}
	}
	return nil
}

func (r *liveRunner) scheduledContains(ctx context.Context, v liveReceipt) (bool, error) {
	cursor := ""
	seen := map[string]bool{"": true}
	for {
		args := []string{"message", "scheduled", "list", "--channel", v.Channel, "--limit", "100"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		data, err := r.read(ctx, args...)
		if err != nil {
			return false, err
		}
		var page struct {
			Rows []struct {
				ID, Channel string
				PostAt      int64  `json:"post_at"`
				Text        string `json:"text_preview"`
			} `json:"scheduled_messages"`
			Paging *livePaging `json:"pagination"`
		}
		if json.Unmarshal(data, &page) != nil || page.Rows == nil || page.Paging == nil {
			return false, errLiveSafety
		}
		for _, row := range page.Rows {
			if row.ID == v.ScheduledID {
				if row.Channel != v.Channel || row.PostAt <= 0 || !strings.Contains(row.Text, r.plan.RunID) {
					return false, errLiveSafety
				}
				return true, nil
			}
		}
		more, err := nextLivePage(*page.Paging, seen)
		if err != nil {
			return false, err
		}
		if !more {
			return false, nil
		}
		cursor = page.Paging.Next
	}
}

// listPages follows real CLI cursors and rejects duplicate or incomplete rows.
func (r *liveRunner) listPages(ctx context.Context, args []string, resource, key string) (map[string]json.RawMessage, int, error) {
	rows := map[string]json.RawMessage{}
	cursor := ""
	seen := map[string]bool{"": true}
	for page := 1; page <= 100; page++ {
		request := slices.Clone(args)
		if cursor != "" {
			request = append(request, "--cursor", cursor)
		}
		data, err := r.read(ctx, request...)
		if err != nil {
			return nil, page, err
		}
		var fields map[string]json.RawMessage
		var items []json.RawMessage
		var paging *livePaging
		if json.Unmarshal(data, &fields) != nil || json.Unmarshal(fields[resource], &items) != nil || items == nil || json.Unmarshal(fields["pagination"], &paging) != nil || paging == nil {
			return nil, page, errors.New("CLI list lost rows or pagination")
		}
		for _, item := range items {
			var values map[string]json.RawMessage
			var id string
			if json.Unmarshal(item, &values) != nil || json.Unmarshal(values[key], &id) != nil || id == "" {
				return nil, page, errors.New("CLI list lost row identity")
			}
			if _, duplicate := rows[id]; duplicate {
				return nil, page, errors.New("CLI pagination duplicated a row")
			}
			rows[id] = item
		}
		more, err := nextLivePage(*paging, seen)
		if err != nil {
			return nil, page, errors.New("CLI list continuation is missing or repeated")
		}
		if !more {
			return rows, page, nil
		}
		cursor = paging.Next
	}
	return nil, 100, errors.New("CLI list exceeded bounded page budget")
}

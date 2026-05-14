package output_test

// Field-order enforcement.
//
// This test walks every Go source file under internal/cli/**/*.go and
// cmd/slick/**/*.go, finds every chain that terminates in event.Msg(...)
// (every plain-mode renderer call), reconstructs the field-key emission
// order from the AST, and verifies the keys are emitted in non-decreasing
// canonical-category order.
//
// Categories: 1=where, 2=what, 3=when, 4=state, 5=detail, 6=numbers,
// 7=diagnostics, 8=pagination. See the comment above BuildBaseLoggers in
// output.go for the canonical narrative.
//
// Strategy: instead of walking a function body and collapsing every
// emission into one sequence, the walker scopes each check to a single
// chain. A "chain" is the receiver-side spine of a single Msg call —
// e.g. event.Str("a", v).Bool("b", v2).Msg(label) is one chain. A
// function with three branches that each terminate in their own Msg has
// three independent chains, checked separately.
//
// To recover the chain across an assignment break (event = X; event = event.Foo(); event.Msg()),
// the walker tracks a per-function map from local variable name to the
// emissions accumulated on that variable so far. Each AssignStmt that
// chains into a known event-typed variable extends its emission list;
// each Msg call on the variable closes a chain and triggers the
// non-decreasing check.
//
// Known limitations:
//   - Helpers that emit fields in OTHER packages — anything outside
//     internal/cli/output's recognized list (AddBoolField, AddIntField,
//     AddSlackTimestampFields, AddPaginationFields) — are opaque. If you
//     add such a helper, either inline it or extend helperEmissions.
//   - Field keys built with non-literal expressions (e.g.
//     event.Str(prefix+"foo", v)) are skipped silently. None exist in
//     this codebase today; if one shows up, the test will simply not
//     see it.
//   - Cross-function tracking: a helper that takes *clog.Event and
//     chains fields onto it is treated as opaque. The recognized four
//     are the carve-out.
//   - Chains that span more than one local variable (rare; not present
//     today) are not stitched together. Each variable's chain is checked
//     in isolation against its own Msg.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalCategory maps every emitted field key to its canonical category.
// Adding a new field key? Pick the right category from the comment above
// BuildBaseLoggers in output.go and put it here. The test fails noisily if
// it sees a key that isn't in the table.
//
//nolint:gochecknoglobals // table-driven test fixture
var canonicalCategory = map[string]int{
	// 1. where
	"workspace": 1, "team_id": 1, "team_name": 1, "profile": 1,
	"channel": 1, "user": 1, "path": 1, "service": 1,
	// 2. what
	"ts": 2, "id": 2, "scheduled_message_id": 2, "name": 2, "type": 2,
	"token_type": 2, "resource": 2, "key": 2, "query": 2,
	// 3. when
	"age": 3, "time": 3, "fetched_at": 3, "post_at": 3, "post_at_iso": 3,
	"expiration": 3, "updated": 3,
	// 4. state
	"authenticated": 4, "valid": 4, "is_member": 4, "is_archived": 4,
	"is_im": 4, "deleted": 4, "removed": 4, "cleared": 4, "written": 4,
	"exists": 4, "dry_run": 4, "from_cache": 4, "truncated": 4,
	"attribution": 4, "command": 4, "healthy": 4, "status": 4, "api_ok": 4,
	"ok": 4,
	// 5. detail
	"text": 5, "topic": 5, "emoji": 5, "presence": 5, "status_text": 5,
	"timezone": 5, "permalink": 5, "description": 5, "value": 5,
	"default_workspace": 5, "thread_ts": 5, "text_preview": 5,
	// 6. numbers
	"count": 6, "members": 6, "num_members": 6, "size": 6, "replies": 6,
	"removed_count": 6, "settings": 6, "expires_in": 6, "resources": 6,
	"active_incidents": 6, "total_active_incidents": 6,
	// 7. diagnostics
	"validation_error": 7, "exit_code": 7,
	// 8. pagination
	"cursor": 8, "next_cursor": 8, "has_more": 8, "max_items": 8,
	"items_returned": 8,
}

// chainMethods are clog.Event accessor methods that emit a single field
// keyed by their first string-literal argument.
//
//nolint:gochecknoglobals // table-driven test fixture
var chainMethods = map[string]bool{
	"Str": true, "Bool": true, "Bytes": true, "Base64": true,
	"Int": true, "Int8": true, "Int16": true, "Int32": true, "Int64": true,
	"Uint": true, "Uint8": true, "Uint16": true, "Uint32": true, "Uint64": true,
	"Float32": true, "Float64": true,
	"Time": true, "Link": true,
}

// emission records a single field-key emission and the source position that
// produced it.
type emission struct {
	key string
	pos token.Position
}

func TestPlainRendererFieldOrderIsCanonical(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	roots := []string{
		filepath.Join(root, "internal", "cli"),
		filepath.Join(root, "cmd", "slick"),
	}
	fset := token.NewFileSet()

	for _, base := range roots {
		walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				t.Errorf("%s: parse: %v", path, parseErr)
				return nil
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				checkFunction(t, fset, fn)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", base, walkErr)
		}
	}
}

// repoRoot returns the slack-cli repo root by climbing from the test's
// working directory until it finds a go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod walking up from %s", dir)
		}
		dir = parent
	}
}

// checkFunction walks a function looking for chains terminated by Msg.
// A "chain" can either be:
//   - a single CallExpr like X.Foo("a").Bar("b").Msg(label) — chained
//     directly off a starter (logger.Info() / c.ResultEvent(...) / etc.).
//   - or a chain split across statements via a local variable
//     (event = c.ResultEvent(); event = event.Str(...); event.Msg(...)).
//
// For the first form the chain is reconstructed from the Msg CallExpr
// alone. For the second form we track per-variable accumulated emissions:
// every assignment to event extends its slice, and every event.Msg(...)
// closes the chain.
func checkFunction(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()
	tracker := map[string][]emission{}
	walkBody(t, fset, fn, fn.Body, tracker)
}

func walkBody(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, body *ast.BlockStmt, tracker map[string][]emission) {
	for _, stmt := range body.List {
		walkStmt(t, fset, fn, stmt, tracker)
	}
}

func walkStmt(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, stmt ast.Stmt, tracker map[string][]emission) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		// Plain expression statement, e.g. event.Msg(...).
		handleExpr(t, fset, fn, s.X, tracker)
	case *ast.AssignStmt:
		// event := c.ResultEvent(...).Str("a", v) and friends.
		handleAssign(t, fset, fn, s, tracker)
	case *ast.IfStmt:
		// Each branch gets its own copy of the tracker so a chain that
		// only exists inside the if doesn't leak out.
		if s.Init != nil {
			walkStmt(t, fset, fn, s.Init, tracker)
		}
		walkBody(t, fset, fn, s.Body, copyTracker(tracker))
		if s.Else != nil {
			walkStmt(t, fset, fn, s.Else, copyTracker(tracker))
		}
	case *ast.BlockStmt:
		walkBody(t, fset, fn, s, tracker)
	case *ast.ForStmt:
		if s.Body != nil {
			walkBody(t, fset, fn, s.Body, copyTracker(tracker))
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			walkBody(t, fset, fn, s.Body, copyTracker(tracker))
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			for _, inner := range s.Body.List {
				walkStmt(t, fset, fn, inner, copyTracker(tracker))
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			for _, inner := range s.Body.List {
				walkStmt(t, fset, fn, inner, copyTracker(tracker))
			}
		}
	case *ast.CaseClause:
		for _, inner := range s.Body {
			walkStmt(t, fset, fn, inner, tracker)
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Results {
			handleExpr(t, fset, fn, expr, tracker)
		}
	}
}

func copyTracker(in map[string][]emission) map[string][]emission {
	out := make(map[string][]emission, len(in))
	for k, v := range in {
		copied := make([]emission, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// handleAssign processes "event := X" / "event = X" so we can extend the
// per-variable emission accumulator across statements.
func handleAssign(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, assign *ast.AssignStmt, tracker map[string][]emission) {
	t.Helper()
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		// Multi-value assignments don't produce event chains.
		handleExpr(t, fset, fn, assign.Rhs[0], tracker)
		return
	}
	lhs, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		handleExpr(t, fset, fn, assign.Rhs[0], tracker)
		return
	}
	rhs := assign.Rhs[0]
	// Walk the RHS chain to collect new emissions and discover the base
	// receiver. If the base receiver is a known variable in tracker,
	// extend its emission list rather than starting fresh.
	chain := chainEmissions(rhs, fset, tracker)
	tracker[lhs.Name] = chain
}

// handleExpr processes a free-standing expression (typically an
// ExprStmt). If it terminates in Msg, run the canonical-order check;
// otherwise propagate any side effects (in practice none).
func handleExpr(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, expr ast.Expr, tracker map[string][]emission) {
	t.Helper()
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if sel.Sel.Name != "Msg" {
		// Could be a non-Msg expression; still walk it so chained closure
		// bodies (event.When(...)) inside log calls aren't missed if they
		// terminate in a Msg of their own.
		ast.Inspect(expr, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			s, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || s.Sel.Name != "Msg" {
				return true
			}
			emissions := chainEmissions(s.X, fset, tracker)
			checkOrder(t, fset, fn, emissions)
			return false
		})
		return
	}
	// Direct Msg call: collect from the receiver chain.
	emissions := chainEmissions(sel.X, fset, tracker)
	checkOrder(t, fset, fn, emissions)
}

// chainEmissions returns the sequence of emissions produced by an
// expression that's the receiver of a chained method call. The receiver
// could be:
//   - a CallExpr (e.g. event.Str("a", v) — emit "a" plus whatever its
//     own receiver emits)
//   - an Ident referring to a tracked event variable (e.g. event — return
//     the tracker's accumulated emissions)
//   - anything else: no emissions.
func chainEmissions(expr ast.Expr, fset *token.FileSet, tracker map[string][]emission) []emission {
	switch e := expr.(type) {
	case *ast.CallExpr:
		var receiverEmissions []emission
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			receiverEmissions = chainEmissions(sel.X, fset, tracker)
		}
		out := append([]emission{}, receiverEmissions...)
		out = append(out, emissionsFromCall(e, fset, tracker)...)
		return out
	case *ast.Ident:
		if existing, ok := tracker[e.Name]; ok {
			out := make([]emission, len(existing))
			copy(out, existing)
			return out
		}
	}
	return nil
}

// emissionsFromCall inspects a single CallExpr and returns the emissions
// it produces. Receivers are NOT walked here — chainEmissions handles
// that. Recognized forms:
//
//   - <event>.Str("x", v) and friends — emit "x".
//   - clioutput.AddBoolField(event, "x", v) / AddIntField — emit "x".
//   - clioutput.AddSlackTimestampFields(event, ts, now) — emit "ts" (verbose
//     mode also emits "time", but that's not enforced for ordering here).
//   - clioutput.AddPaginationFields(event, pag) — emit cursor, next_cursor,
//     has_more, max_items, items_returned (always last in canonical order).
//   - <event>.When(cond, func(e *clog.Event) { ... }) — descend into the
//     closure body and inline its emissions in source order.
//
// Anything else returns nil.
func emissionsFromCall(call *ast.CallExpr, fset *token.FileSet, tracker map[string][]emission) []emission {
	pos := fset.Position(call.Lparen)
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		name := fn.Sel.Name
		if chainMethods[name] && len(call.Args) >= 1 {
			if key, ok := stringLit(call.Args[0]); ok {
				return []emission{{key: key, pos: pos}}
			}
			return nil
		}
		if name == "When" && len(call.Args) >= 2 {
			lit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || lit.Body == nil {
				return nil
			}
			out := []emission{}
			for _, stmt := range lit.Body.List {
				out = append(out, emissionsFromClosureStmt(stmt, fset, tracker)...)
			}
			return out
		}
		// Cross-package helper calls: clioutput.AddBoolField(event, "x", v).
		if pkgIdent, ok := fn.X.(*ast.Ident); ok {
			return helperEmissions(pkgIdent.Name, name, call, fset)
		}
		return nil
	case *ast.Ident:
		// Same-package helper calls (rare, but support them).
		return helperEmissions("", fn.Name, call, fset)
	}
	return nil
}

// emissionsFromClosureStmt collects emissions inside a When-closure body,
// where the receiver of every chained call is the closure parameter `e`.
// We descend chain-wise just like the top-level walker.
func emissionsFromClosureStmt(stmt ast.Stmt, fset *token.FileSet, tracker map[string][]emission) []emission {
	out := []emission{}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		out = append(out, closureExprEmissions(s.X, fset, tracker)...)
	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			out = append(out, closureExprEmissions(rhs, fset, tracker)...)
		}
	case *ast.IfStmt:
		if s.Body != nil {
			for _, inner := range s.Body.List {
				out = append(out, emissionsFromClosureStmt(inner, fset, tracker)...)
			}
		}
		if s.Else != nil {
			out = append(out, emissionsFromClosureStmt(s.Else, fset, tracker)...)
		}
	case *ast.BlockStmt:
		for _, inner := range s.List {
			out = append(out, emissionsFromClosureStmt(inner, fset, tracker)...)
		}
	}
	return out
}

func closureExprEmissions(expr ast.Expr, fset *token.FileSet, tracker map[string][]emission) []emission {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	out := []emission{}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		out = append(out, closureExprEmissions(sel.X, fset, tracker)...)
	}
	out = append(out, emissionsFromCall(call, fset, tracker)...)
	return out
}

// helperEmissions covers the four cross-package helpers in
// internal/cli/output that emit fields onto a passed-in *clog.Event.
func helperEmissions(_, name string, call *ast.CallExpr, fset *token.FileSet) []emission {
	pos := fset.Position(call.Lparen)
	switch name {
	case "AddBoolField", "AddIntField":
		// signature: (event, key, value)
		if len(call.Args) >= 2 {
			if key, ok := stringLit(call.Args[1]); ok {
				return []emission{{key: key, pos: pos}}
			}
		}
		return nil
	case "AddSlackTimestampFields":
		// emits ts (cat 2). time is verbose-only and skipped here.
		return []emission{
			{key: "ts", pos: pos},
		}
	case "AddPaginationFields":
		// always-last canonical pagination footer
		return []emission{
			{key: "cursor", pos: pos},
			{key: "next_cursor", pos: pos},
			{key: "has_more", pos: pos},
			{key: "max_items", pos: pos},
			{key: "items_returned", pos: pos},
		}
	}
	return nil
}

// stringLit returns the unquoted value of a basic string literal.
func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	if len(lit.Value) < 2 {
		return "", false
	}
	return lit.Value[1 : len(lit.Value)-1], true
}

// checkOrder verifies the emission sequence is non-decreasing in canonical
// category. Reports every violation as a t.Errorf; does not stop at the
// first one so a single test run reports all issues.
func checkOrder(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, emissions []emission) {
	t.Helper()
	receiverName := funcQualifiedName(fn)
	prevCat := 0
	prevKey := ""
	prevPos := token.Position{}
	for _, em := range emissions {
		cat, ok := canonicalCategory[em.key]
		if !ok {
			t.Errorf("%s: %s — field %q has no canonical category. Add it to canonicalCategory in field_order_test.go.",
				em.pos, receiverName, em.key)
			continue
		}
		if cat < prevCat {
			t.Errorf("%s: %s — field %q (cat %d) emitted after %q (cat %d, %s)",
				em.pos, receiverName, em.key, cat, prevKey, prevCat, prevPos)
		}
		prevCat = cat
		prevKey = em.key
		prevPos = em.pos
	}
}

// funcQualifiedName returns "ReceiverType.MethodName" for methods, or just
// "FuncName" for free functions. Used in error messages so a violation is
// easy to grep.
func funcQualifiedName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if ident, ok := t.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

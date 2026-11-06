package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/matcra587/slack-cli/internal/slackcatalog"
)

type sdkMethod struct {
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Endpoints []string `json:"endpoints,omitempty"`
}

type sdkInventory struct {
	Version    string      `json:"version"`
	SourceHash string      `json:"source_hash"`
	Source     string      `json:"source"`
	Note       string      `json:"note"`
	Methods    []sdkMethod `json:"methods"`
}

type sdkFunction struct {
	method sdkMethod
	calls  []string
}

func scanSDK(directory, version string) (sdkInventory, error) {
	result := sdkInventory{
		Version: version, Source: "https://github.com/slack-go/slack/tree/" + version,
		Note: "Static call-graph inventory of exported *Client methods. Endpoints are resolved from request-builder literals and reachable helpers; an empty list is not proof that a method makes no network calls.",
	}
	paths, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		return result, err
	}
	functions := make(map[string]sdkFunction)
	var exports []string
	constants := make(map[string]string)
	var sourceBytes []byte
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path) //nolint:gosec // Source files come from the explicitly selected SDK source directory.
		if err != nil {
			return result, err
		}
		sourceBytes = append(sourceBytes, raw...)
		f, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			return result, err
		}
		for _, decl := range f.Decls {
			if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, spec := range g.Specs {
					if v, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range v.Names {
							if i < len(v.Values) {
								if endpoint := endpointLiteral(v.Values[i]); endpoint != "" {
									constants[name.Name] = endpoint
								}
							}
						}
					}
				}
			}
		}
	}
	result.SourceHash = hash(sourceBytes)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return result, fmt.Errorf("parsing SDK source: %w", err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			if function.Recv != nil {
				pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				receiver, ok := pointer.X.(*ast.Ident)
				if !ok || receiver.Name != "Client" {
					continue
				}
				if ast.IsExported(name) {
					exports = append(exports, name)
				}
			}
			entry := sdkFunction{method: sdkMethod{Name: name, File: filepath.Base(path), Line: fileSet.Position(function.Pos()).Line}}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if value, ok := node.(*ast.BasicLit); ok {
					if endpoint := endpointLiteral(value); endpoint != "" {
						entry.method.Endpoints = append(entry.method.Endpoints, endpoint)
					}
				}
				if value, ok := node.(*ast.Ident); ok {
					if endpoint := constants[value.Name]; endpoint != "" {
						entry.method.Endpoints = append(entry.method.Endpoints, endpoint)
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				called := functionName(call.Fun)
				entry.calls = append(entry.calls, called)
				for _, argument := range call.Args {
					if endpoint := endpointLiteral(argument); endpoint != "" && requestBuilder(called) {
						entry.method.Endpoints = append(entry.method.Endpoints, endpoint)
					}
				}
				return true
			})
			functions[name] = entry
		}
	}
	// These methods defer requests to pagination objects or use Slack's legacy
	// workspace URL builder. Their endpoint strings are not reachable through
	// the ordinary *Client call graph above. Keep these mappings explicit.
	for name, endpoints := range deferredEndpoints {
		if function, ok := functions[name]; ok {
			function.method.Endpoints = append(function.method.Endpoints, endpoints...)
			functions[name] = function
		}
	}
	sort.Strings(exports)
	for _, name := range slices.Compact(exports) {
		entry := functions[name].method
		entry.Endpoints = reachableEndpoints(name, functions, make(map[string]bool))
		sort.Strings(entry.Endpoints)
		entry.Endpoints = slices.Compact(entry.Endpoints)
		result.Methods = append(result.Methods, entry)
	}
	return result, nil
}

func functionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.Ident:
		return value.Name
	}
	return ""
}

func requestBuilder(name string) bool {
	return name == "postMethod" || name == "getMethod" || name == "postJSON" ||
		name == "getResource" || name == "postForm" || name == "newPostRequest" ||
		strings.Contains(name, "MultipartResponse")
}

func endpointLiteral(expression ast.Expr) string {
	if binary, ok := expression.(*ast.BinaryExpr); ok && binary.Op == token.ADD {
		return endpointLiteral(binary.Y)
	}
	value, ok := expression.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		return ""
	}
	text, err := strconv.Unquote(value.Value)
	if err != nil || !regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(?:\.[a-zA-Z][a-zA-Z0-9]*)+$`).MatchString(text) {
		return ""
	}
	family, _, _ := strings.Cut(text, ".")
	if !strings.Contains("|admin|agents|api|apps|assistant|auth|bookmarks|bots|calls|canvases|channels|chat|conversations|dialog|dnd|emoji|entity|files|functions|groups|im|migration|mpim|oauth|openid|pins|reactions|reminders|rtm|search|slackbot|slackLists|stars|team|tooling|usergroups|users|views|workflows|", "|"+family+"|") {
		return ""
	}
	return text
}

var deferredEndpoints = map[string][]string{
	"GetConversationsPaginated":  {"conversations.list"},
	"GetUsersPaginated":          {"users.list"},
	"ListStarsPaginated":         {"stars.list"},
	"DisableUserContext":         {"users.admin.setInactive"},
	"InviteGuestContext":         {"users.admin.invite"},
	"InviteRestrictedContext":    {"users.admin.invite"},
	"InviteToTeamContext":        {"users.admin.invite"},
	"SendSSOBindingEmailContext": {"users.admin.sendSSOBind"},
	"SetRegularContext":          {"users.admin.setRegular"},
	"SetRestrictedContext":       {"users.admin.setRestricted"},
	"SetUltraRestrictedContext":  {"users.admin.setUltraRestricted"},
}

func reachableEndpoints(name string, functions map[string]sdkFunction, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	function := functions[name]
	result := append([]string(nil), function.method.Endpoints...)
	for _, called := range function.calls {
		result = append(result, reachableEndpoints(called, functions, seen)...)
	}
	return result
}

func joinSDK(methods []slackcatalog.Method, inventory sdkInventory) []slackcatalog.Method {
	byName := make(map[string]*slackcatalog.Method, len(methods))
	for i := range methods {
		byName[methods[i].Name] = &methods[i]
	}
	sdkOnly := make(map[string]slackcatalog.Method)
	for _, function := range inventory.Methods {
		for _, endpoint := range function.Endpoints {
			if method, ok := byName[endpoint]; ok {
				method.SDKMethods = append(method.SDKMethods, function.Name)
				continue
			}
			method := sdkOnly[endpoint]
			method.Name = endpoint
			method.Description = "Endpoint referenced by the pinned Go SDK; not indexed in Slack's current method documentation."
			method.Source = "sdk"
			method.SourceHash = inventory.SourceHash
			method.URL = inventory.Source + "/" + function.File
			method.Pagination = "unknown"
			method.SDKMethods = append(method.SDKMethods, function.Name)
			slackcatalog.ApplyPolicy(&method)
			sdkOnly[endpoint] = method
		}
	}
	for _, method := range sdkOnly {
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
	return methods
}

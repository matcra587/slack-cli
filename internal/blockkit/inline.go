package blockkit

import (
	"html"
	"net/url"
	"strings"
	"unicode"

	"github.com/slack-go/slack/slackutilsx"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// markdownInline escapes user text before adding Slack's formatting delimiters.
// Single emphasis keeps its original delimiter for existing Slack mrkdwn users.
func markdownInline(node ast.Node, source []byte) string {
	var out strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *ast.Text:
			out.WriteString(slackutilsx.EscapeMessage(string(typed.Segment.Value(source))))
			if typed.SoftLineBreak() || typed.HardLineBreak() {
				out.WriteByte('\n')
			}
		case *ast.String:
			out.WriteString(slackutilsx.EscapeMessage(string(typed.Value)))
		case *ast.Emphasis:
			marker := "*"
			if typed.Level == 1 && typed.Pos() >= 0 && typed.Pos() < len(source) && source[typed.Pos()] == '_' {
				marker = "_"
			}
			out.WriteString(marker + markdownInline(typed, source) + marker)
		case *extast.Strikethrough:
			out.WriteString("~" + markdownInline(typed, source) + "~")
		case *ast.CodeSpan:
			out.WriteByte('`')
			out.WriteString(markdownInline(typed, source))
			out.WriteByte('`')
		case *ast.Link:
			out.WriteString(markdownLink(typed, source))
		case *ast.AutoLink:
			out.WriteString(slackutilsx.EscapeMessage("<" + string(typed.Label(source)) + ">"))
		case *ast.RawHTML:
			out.WriteString(slackutilsx.EscapeMessage(string(typed.Segments.Value(source))))
		case *ast.Image:
			out.WriteString("![" + markdownInline(typed, source) + "](" + slackutilsx.EscapeMessage(string(typed.Destination)) + ")")
		default:
			out.WriteString(markdownInline(child, source))
		}
	}
	return out.String()
}

func markdownLink(link *ast.Link, source []byte) string {
	label := markdownInline(link, source)
	destination := html.UnescapeString(string(util.UnescapePunctuations(link.Destination)))
	fallback := "[" + label + "](" + slackutilsx.EscapeMessage(destination) + ")"
	parsed, err := url.Parse(destination)
	if err != nil {
		return fallback
	}
	validTarget := ((parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "") ||
		(parsed.Scheme == "mailto" && parsed.Opaque != "")
	if !validTarget || strings.IndexFunc(destination, unicode.IsSpace) >= 0 || strings.Contains(label, "|") {
		return fallback
	}
	// These characters delimit Slack's link/mention syntax, including in queries.
	target := strings.NewReplacer("<", "%3C", ">", "%3E", "|", "%7C").Replace(parsed.String())
	target = slackutilsx.EscapeMessage(target)
	if label == "" {
		return "<" + target + ">"
	}
	return "<" + target + "|" + label + ">"
}

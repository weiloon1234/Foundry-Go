// Package markdown supplies the shared, source-located documentation reader.
package markdown

import (
	"bytes"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
)

// Lines indexes newline offsets once, including the final unterminated line.
type Lines struct{ starts []int }

// NewLines makes one pass over the source; lookups never recount preceding newlines.
func NewLines(data []byte) Lines {
	l := Lines{starts: []int{0}}
	for i, b := range data {
		if b == '\n' {
			l.starts = append(l.starts, i+1)
		}
	}
	return l
}

// Number returns a one-based source line for a byte offset.
func (l Lines) Number(offset int) int {
	return sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > offset })
}

// Start returns the byte offset of the containing line.
func (l Lines) Start(offset int) int { return l.starts[max(0, l.Number(offset)-1)] }

// Kind distinguishes syntax that has different teaching rules.
type Kind uint8

const (
	// Prose contains ordinary paragraph or tight-list content.
	Prose Kind = iota
	// Fence contains the info string and decontainerized fenced body.
	Fence
	// Indented is a parser-recognized indented code block.
	Indented
	// HTML retains raw element syntax, including block contents.
	HTML
	// Comment is HTML comment syntax, never executable teaching.
	Comment
	// Heading carries its level and title.
	Heading
	// CodeSpan contains a parsed inline code literal.
	CodeSpan
	// Link includes resolved reference links, images and autolinks.
	Link
	// Text is ordinary inline content outside code and HTML.
	Text
)

// Node retains the parser's container identity and source range. Text excludes
// quote/list prefixes; fence offsets delimit only the replaceable body.
type Node struct {
	// AST preserves sibling and container identity for binding adjacency.
	AST ast.Node
	// Kind selects the document rule applicable to this node.
	Kind Kind
	// First and Last are inclusive, one-based source lines.
	First, Last int
	// Start and End delimit the node's source content in bytes.
	Start, End int
	// Text omits Markdown containers; Info retains the complete fence info string.
	Text, Info string
	// Destination is the resolved target of a parsed link.
	Destination string
	// Level and ATX distinguish heading depth and hash-prefixed headings.
	Level int
	ATX   bool
	// Quoted and Listed record enclosing Markdown containers.
	Quoted, Listed bool
	// BodyStart and BodyEnd delimit the replaceable body, excluding delimiters.
	BodyStart, BodyEnd int
	// Closed distinguishes a delimiter close from EOF or a container ending.
	Closed bool
	// Prefix is the parser-observed container prefix for rendered body lines.
	Prefix string
}

// Document keeps block order and inline syntax from one CommonMark/GFM parse.
type Document struct {
	// Source supplies original bytes for segment-based prose projections.
	Source []byte
	// Blocks and Inlines preserve source order within each syntax category.
	Blocks  []Node
	Inlines []Node
	// Lines maps source offsets to their original line numbers.
	Lines Lines
}

// Read delegates Markdown recognition, including containers and references, to
// goldmark. The fence adapter records offsets without recognizing syntax itself.
func Read(data []byte) Document {
	result := Document{Source: data, Lines: NewLines(data)}
	fences := &fenceParser{BlockParser: parser.NewFencedCodeBlockParser(), nodes: make(map[ast.Node]*Node), lines: result.Lines, source: data}
	blocks := parser.DefaultBlockParsers()
	for i := range blocks {
		if blocks[i].Priority == 700 {
			blocks[i].Value = fences
		}
	}
	p := parser.NewParser(parser.WithBlockParsers(blocks...), parser.WithInlineParsers(parser.DefaultInlineParsers()...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))
	// Tables are the sole GFM extension needed by repository documents.
	p.AddOptions(parser.WithParagraphTransformers(util.Prioritized(extension.NewTableParagraphTransformer(), 200)), parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)))
	root := p.Parse(text.NewReader(data))
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		item := Node{AST: n, Start: n.Pos()}
		for parent := n.Parent(); parent != nil; parent = parent.Parent() {
			switch parent.Kind() {
			case ast.KindBlockquote:
				item.Quoted = true
			case ast.KindListItem:
				item.Listed = true
			}
		}
		inline := false
		switch v := n.(type) {
		case *ast.FencedCodeBlock:
			saved := fences.nodes[n]
			item.Start, item.End = saved.Start, saved.End
			item.BodyStart, item.BodyEnd = saved.BodyStart, saved.BodyEnd
			item.Prefix, item.Closed = saved.Prefix, saved.Closed
			item.Kind = Fence
			item.Text = string(v.Lines().Value(data))
			if v.Info != nil {
				item.Info = string(v.Info.Segment.Value(data))
			}
		case *ast.CodeBlock:
			item.Kind = Indented
			item.Text = string(v.Lines().Value(data))
		case *ast.HTMLBlock:
			item.Kind = HTML
			item.Text = string(v.Lines().Value(data))
			if v.HasClosure() {
				item.Text += string(v.ClosureLine.Value(data))
				item.End = v.ClosureLine.Stop
			}
			if v.HTMLBlockType == ast.HTMLBlockType2 {
				item.Kind = Comment
			}
		case *ast.Heading:
			item.ATX = n.Pos() >= 0 && n.Pos() < len(data) && data[n.Pos()] == '#'
			item.Kind = Heading
			item.Level = v.Level
			item.Text = string(v.Text(data))
		case *ast.Paragraph, *ast.TextBlock:
			item.Kind = Prose
			item.Text = string(n.Lines().Value(data))
		case *ast.RawHTML:
			inline = true
			item.Kind = HTML
			item.Text = string(v.Segments.Value(data))
			if v.Segments.Len() > 0 {
				item.Start = v.Segments.At(0).Start
				item.End = v.Segments.At(v.Segments.Len() - 1).Stop
			}
			if strings.HasPrefix(item.Text, "<!--") {
				item.Kind = Comment
			}
		case *ast.CodeSpan:
			inline = true
			item.Kind = CodeSpan
			item.Text = string(v.Text(data))
			item.End = inlineEnd(n)
		case *ast.Link:
			inline = true
			item.Kind = Link
			item.Destination = string(v.Destination)
			item.Text = string(v.Text(data))
			item.End = inlineEnd(n)
		case *ast.Image:
			inline = true
			item.Kind = Link
			item.Destination = string(v.Destination)
			item.End = inlineEnd(n)
		case *ast.AutoLink:
			inline = true
			item.Kind = Link
			item.Destination = string(v.URL(data))
			item.End = item.Start + len(v.Label(data))
		case *ast.Text:
			inline = true
			item.Kind = Text
			item.Text = string(v.Segment.Value(data))
			item.Start = v.Segment.Start
			item.End = v.Segment.Stop
		case *ast.String:
			inline = true
			item.Kind = Text
			item.Text = string(v.Value)
			item.End = item.Start + len(v.Value)
		default:
			return ast.WalkContinue, nil
		}
		if !inline && item.Kind != Fence && n.Lines().Len() > 0 {
			item.Start = n.Lines().At(0).Start
			item.End = max(item.End, n.Lines().At(n.Lines().Len()-1).Stop)
		}
		item.Start = max(0, item.Start)
		item.End = max(item.Start, item.End)
		item.First = result.Lines.Number(item.Start)
		item.Last = result.Lines.Number(max(item.Start, item.End-1))
		if inline {
			result.Inlines = append(result.Inlines, item)
		} else {
			result.Blocks = append(result.Blocks, item)
		}
		if item.Kind == CodeSpan {
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return result
}

func inlineEnd(n ast.Node) int {
	end := max(0, n.Pos())
	_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := child.(*ast.Text); ok {
				end = max(end, t.Segment.Stop)
			}
		}
		return ast.WalkContinue, nil
	})
	return end
}

type fenceParser struct {
	parser.BlockParser
	nodes  map[ast.Node]*Node
	lines  Lines
	source []byte
}

func (p *fenceParser) Open(parent ast.Node, r text.Reader, pc parser.Context) (ast.Node, parser.State) {
	_, segment := r.PeekLine()
	offset := pc.BlockOffset()
	n, state := p.BlockParser.Open(parent, r, pc)
	if n != nil {
		p.nodes[n] = &Node{Start: segment.Start - segment.Padding + offset, End: segment.Stop, BodyStart: segment.Stop, BodyEnd: segment.Stop}
	}
	return n, state
}
func (p *fenceParser) Continue(n ast.Node, r text.Reader, pc parser.Context) parser.State {
	line, segment := r.PeekLine()
	state := p.BlockParser.Continue(n, r, pc)
	item := p.nodes[n]
	if state&parser.Close != 0 {
		item.Closed = true
		item.BodyEnd = p.lines.Start(segment.Start)
		item.End = segment.Stop
		delimiter := segment.Start - segment.Padding + util.FirstNonSpacePosition(line)
		item.Prefix = string(p.source[item.BodyEnd:delimiter])
	} else {
		item.End = segment.Stop
		item.BodyEnd = segment.Stop
	}
	return state
}

// HasCodeElement tokenizes HTML so comments and attribute values cannot masquerade
// as pre/code elements. Markdown decides which bytes are HTML in the first place.
func HasCodeElement(raw string) bool {
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if bytes.EqualFold(name, []byte("pre")) || bytes.EqualFold(name, []byte("code")) {
				return true
			}
		}
	}
}

// HTMLTokens exposes element and comment locations within a Markdown HTML node.
func HTMLTokens(node Node, visit func(html.TokenType, string, int)) {
	z := html.NewTokenizer(strings.NewReader(node.Text))
	line := node.First
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return
		}
		raw := string(z.Raw())
		visit(kind, raw, line)
		line += strings.Count(raw, "\n")
	}
}

// ProseLines retains inline code for declaration grammars while masking only
// comment bytes. A comment cannot hide ordinary text elsewhere on its line.
func (d Document) ProseLines(visit func(int, string)) {
	var comments []Node
	for _, node := range d.Inlines {
		if node.Kind == Comment {
			comments = append(comments, node)
		}
	}
	cursor := 0
	for _, node := range d.Blocks {
		if node.Kind != Prose {
			continue
		}
		for i := 0; i < node.AST.Lines().Len(); i++ {
			segment := node.AST.Lines().At(i)
			line := append([]byte(nil), segment.Value(d.Source)...)
			for cursor < len(comments) && comments[cursor].End <= segment.Start {
				cursor++
			}
			for j := cursor; j < len(comments) && comments[j].Start < segment.Stop; j++ {
				start := max(0, comments[j].Start-segment.Start+segment.Padding)
				end := min(len(line), comments[j].End-segment.Start+segment.Padding)
				for k := start; k < end; k++ {
					if line[k] != '\n' {
						line[k] = ' '
					}
				}
			}
			visit(d.Lines.Number(segment.Start), strings.TrimSuffix(string(line), "\n"))
		}
	}
}

// AnchorLines yields only lines containing actual anchor elements, never comments
// or code spans. The consumer owns the closed explicit-anchor spelling.
func (d Document) AnchorLines(visit func(int, string)) {
	lines := make(map[int]string)
	for _, n := range d.Blocks {
		if n.Kind == HTML || n.Kind == Prose || n.Kind == Heading {
			for i, line := range strings.Split(n.Text, "\n") {
				lines[n.First+i] = line
			}
		}
	}
	seen := make(map[int]bool)
	nodes := append(append([]Node(nil), d.Blocks...), d.Inlines...)
	for _, n := range nodes {
		if n.Kind != HTML {
			continue
		}
		HTMLTokens(n, func(kind html.TokenType, raw string, line int) {
			if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
				return
			}
			z := html.NewTokenizer(strings.NewReader(raw))
			z.Next()
			tag, _ := z.TagName()
			if string(tag) != "a" || seen[line] {
				return
			}
			seen[line] = true
			if value, ok := lines[line]; ok {
				visit(line, value)
			}
		})
	}
}

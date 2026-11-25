// Package markdown supplies the shared, source-located documentation reader.
package markdown

import (
	"bytes"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
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
	// TableRow carries the plain text of each cell in a header or body row.
	TableRow
)

// Node retains the parser's container identity and source range. Text excludes
// quote/list prefixes; fence offsets delimit only the replaceable body.
type Node struct {
	// syntax is the parser node, kept so the walk can resolve sibling adjacency
	// without exposing the parser's types.
	syntax ast.Node
	// Next and Previous index Document.Blocks for the adjacent sibling in the
	// same container, or are -1 when that sibling is not a block in Blocks.
	Next, Previous int
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
	// Prefix is the container prefix for rendered body lines: the opener's own
	// prefix with list markers replaced by the spaces that continue the item.
	Prefix string
	// ClosePrefix is the container prefix observed on the closing delimiter line.
	ClosePrefix string
	// Cells holds the plain text of each cell of a TableRow.
	Cells []string
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

// Read refuses inputs the parser's cost cannot be bounded for, then delegates
// Markdown recognition, including containers and references, to goldmark. The
// fence adapter records offsets without recognizing syntax itself.
func Read(name string, data []byte) (Document, error) {
	if err := checkShape(name, data); err != nil {
		return Document{}, err
	}
	document, unclosed := parse(data)
	if unclosed > 0 {
		return Document{}, diagnostic.DocComment.At(name, unclosed, 0).Say("HTML comment", "not closed").To("add \"-->\"").Err()
	}
	return document, nil
}

// fencePriority is goldmark's registration priority for its fenced-code block
// parser. A test fails if the default list stops holding exactly one parser at
// this priority, because the wrapper below would then silently not be installed.
const fencePriority = 700

// parse also returns the line of the first comment with no closing "-->", or 0.
func parse(data []byte) (Document, int) {
	unclosed := 0
	result := Document{Source: data, Lines: NewLines(data)}
	fences := &fenceParser{BlockParser: parser.NewFencedCodeBlockParser(), nodes: make(map[ast.Node]*Node), lines: result.Lines, source: data}
	blocks := parser.DefaultBlockParsers()
	for i := range blocks {
		if blocks[i].Priority == fencePriority {
			blocks[i].Value = fences
		}
	}
	p := parser.NewParser(parser.WithBlockParsers(blocks...), parser.WithInlineParsers(parser.DefaultInlineParsers()...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))
	// Tables are the sole GFM extension needed by repository documents.
	p.AddOptions(parser.WithParagraphTransformers(util.Prioritized(extension.NewTableParagraphTransformer(), 200)), parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)))
	root := p.Parse(text.NewReader(data))
	// Container context is carried down the walk, never recomputed per node.
	quotes, items := 0, 0
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.Kind() {
		case ast.KindBlockquote:
			if !entering {
				quotes--
			}
		case ast.KindListItem:
			if !entering {
				items--
			}
		}
		if !entering {
			return ast.WalkContinue, nil
		}
		item := Node{syntax: n, Next: -1, Previous: -1, Start: n.Pos(), Quoted: quotes > 0, Listed: items > 0}
		switch n.Kind() {
		case ast.KindBlockquote:
			quotes++
		case ast.KindListItem:
			items++
		}
		inline := false
		switch v := n.(type) {
		case *ast.FencedCodeBlock:
			saved := fences.nodes[n]
			item.Start, item.End = saved.Start, saved.End
			item.BodyStart, item.BodyEnd = saved.BodyStart, saved.BodyEnd
			item.Prefix, item.ClosePrefix, item.Closed = saved.Prefix, saved.ClosePrefix, saved.Closed
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
			// A type 2 block ends on the line holding "-->", so text or elements
			// after the comment on that line belong to this block: only a block
			// made of comments alone is a Comment.
			if v.HTMLBlockType == ast.HTMLBlockType2 && onlyComments(item.Text) {
				item.Kind = Comment
			}
			if unclosed == 0 && v.HTMLBlockType == ast.HTMLBlockType2 {
				if offset := unclosedCommentOffset(item.Text); offset >= 0 {
					unclosed = result.Lines.Number(max(0, n.Lines().At(0).Start)) + strings.Count(item.Text[:offset], "\n")
				}
			}
		case *extast.TableRow, *extast.TableHeader:
			item.Kind = TableRow
			for cell := n.FirstChild(); cell != nil; cell = cell.NextSibling() {
				item.Cells = append(item.Cells, strings.TrimSpace(string(cell.Text(data))))
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
	index := make(map[ast.Node]int, len(result.Blocks))
	for i, block := range result.Blocks {
		index[block.syntax] = i
	}
	for i := range result.Blocks {
		if next, ok := index[result.Blocks[i].syntax.NextSibling()]; ok {
			result.Blocks[i].Next = next
		}
		if previous, ok := index[result.Blocks[i].syntax.PreviousSibling()]; ok {
			result.Blocks[i].Previous = previous
		}
	}
	return result, unclosed
}

// unclosedCommentOffset returns the offset of a comment in raw that has no
// closing "-->" (the tokenizer runs it to the end of the input), or -1.
func unclosedCommentOffset(raw string) int {
	z := html.NewTokenizer(strings.NewReader(raw))
	offset := 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return -1
		}
		token := z.Raw()
		if kind == html.CommentToken && !bytes.HasSuffix(token, []byte("-->")) {
			return offset
		}
		offset += len(token)
	}
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
		start := segment.Start - segment.Padding + offset
		p.nodes[n] = &Node{Start: start, End: segment.Stop, BodyStart: segment.Stop, BodyEnd: segment.Stop, Prefix: bodyPrefix(p.source[p.lines.Start(start):start])}
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
		item.ClosePrefix = string(p.source[item.BodyEnd:delimiter])
	} else {
		item.End = segment.Stop
		item.BodyEnd = segment.Stop
	}
	return state
}

// bodyPrefix keeps quote markers and whitespace from the opener's line prefix
// and turns list markers into the spaces that continue their item.
func bodyPrefix(raw []byte) string {
	out := make([]byte, len(raw))
	for i, b := range raw {
		if b == '>' || b == ' ' || b == '\t' {
			out[i] = b
		} else {
			out[i] = ' '
		}
	}
	return string(out)
}

// onlyComments reports whether raw holds nothing but comments and whitespace.
func onlyComments(raw string) bool {
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return true
		case html.CommentToken:
		case html.TextToken:
			if len(bytes.TrimSpace(z.Text())) != 0 {
				return false
			}
		default:
			return false
		}
	}
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
		for i := 0; i < node.syntax.Lines().Len(); i++ {
			segment := node.syntax.Lines().At(i)
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

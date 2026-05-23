package main

import (
	"bytes"
	"fmt"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.yaml.in/yaml/v4"
	"strconv"
	"strings"
	"unicode"
)

// PUBLIC API
//

type HeadingInfo struct {
	Level int
	Text  string
	ID    string

	// TOC streaming information
	OpenUL  int  // number of <ul> to open before this heading
	CloseLI bool // whether the previous <li> must be closed
	CloseUL int  // number of </ul></li> pairs to emit after this heading
}

func HeadingsExtension() goldmark.Extender {
	return &headingsExtension{}
}

func GetHeadings(pc parser.Context) []HeadingInfo {
	v := pc.Get(headingsContextKey)
	if v == nil {
		return nil
	}
	return v.([]HeadingInfo)
}

// Represents the YAML frontmatter schema.
type Frontmatter struct {
	Title    string
	Abstract string
	Macros   map[string]string
}

// Returns a Goldmark extension that extracts YAML
// frontmatter and stores it in the parser context.
func FrontmatterExtension() goldmark.Extender {
	return &frontmatterExtension{}
}

// Retrieves parsed metadata from the Goldmark parser context.
func GetFrontmatter(ctx parser.Context) (Frontmatter, bool) {
	v := ctx.Get(metadataContextKey)
	if v == nil {
		return defaultFrontmatter(), false
	}
	return v.(Frontmatter), true
}

// Returns a Goldmark extension that parses $...$ and $$...$$ math.
func KatexExtension() goldmark.Extender {
	return &katexExtension{}
}

// IMPLEMENTATION OF HEADINGS EXTENSION
//

var headingsContextKey = parser.NewContextKey()

type headingsExtension struct{}

func (e *headingsExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithASTTransformers(
			util.Prioritized(&headingCollector{}, 100),
		),
	)

	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&headingShifter{}, 100),
		),
	)
}

type headingCollector struct{}

func (t *headingCollector) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	var headings []HeadingInfo

	prevLevel := 0
	idCounter := 0

	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		// only collect h1/h2
		if h.Level > 2 {
			return ast.WalkContinue, nil
		}

		text := extractText(h, reader.Source())

		baseID := slugify(text)
		idCounter++
		id := fmt.Sprintf("%d-%s", idCounter, baseID)

		h.SetAttributeString("id", []byte(id))

		cur := h.Level

		diff := cur - prevLevel

		info := HeadingInfo{
			Level: cur,
			Text:  text,
			ID:    id,
		}

		switch {
		case diff > 0:
			info.OpenUL = diff
			info.CloseLI = false

		case diff == 0:
			info.CloseLI = true

		case diff < 0:
			info.CloseLI = true
			info.CloseUL = -diff
		}

		headings = append(headings, info)

		prevLevel = cur

		return ast.WalkContinue, nil
	})

	// close remaining nesting
	if len(headings) > 0 {
		headings[len(headings)-1].CloseUL += prevLevel
	}

	pc.Set(headingsContextKey, headings)
}

func extractText(h *ast.Heading, source []byte) string {
	var buf bytes.Buffer

	ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := n.(*ast.Text); ok {
				buf.Write(t.Segment.Value(source))
			}
		}
		return ast.WalkContinue, nil
	})

	return buf.String()
}

func slugify(s string) string {
	s = strings.ToLower(s)

	var b strings.Builder
	prevDash := false

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "heading"
	}

	return out
}

type headingShifter struct{}

func (r *headingShifter) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, r.shiftAndRender)
}

func (r *headingShifter) shiftAndRender(
	w util.BufWriter,
	source []byte,
	node ast.Node,
	entering bool,
) (ast.WalkStatus, error) {

	h := node.(*ast.Heading)

	level := min(h.Level+1, 6)

	if entering {
		_, _ = fmt.Fprintf(w, "<h%d", level)

		if id, ok := h.AttributeString("id"); ok {
			_, _ = fmt.Fprintf(w, ` id="%s"`, id)
		}

		_, _ = w.WriteString(">")
	} else {
		_, _ = fmt.Fprintf(w, "</h%d>\n", level)
	}

	return ast.WalkContinue, nil
}

////////////////////////////////////////////////////////////////////////////////
// IMPLEMENTATION OF FRONTMATTER EXTENSION
////////////////////////////////////////////////////////////////////////////////

var metadataContextKey = parser.NewContextKey()

func defaultFrontmatter() Frontmatter {
	return Frontmatter{
		Title:    "",
		Abstract: "",
		Macros:   map[string]string{},
	}
}

func normalizeMacros(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(input))
	for k, v := range input {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if !strings.HasPrefix(key, "\\") {
			key = "\\" + key
		}
		out[key] = v
	}

	return out
}

type frontmatterExtension struct{}

func (e *frontmatterExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(
			util.Prioritized(newFrontmatterParser(), 5),
		),
	)
}

type frontmatterParser struct{}

func newFrontmatterParser() parser.BlockParser {
	return &frontmatterParser{}
}

func (p *frontmatterParser) Trigger() []byte {
	return []byte{'-'}
}

func isBlank(line []byte) bool {
	return len(bytes.TrimSpace(line)) == 0
}

func (p *frontmatterParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	if pc.Get(metadataContextKey) != nil {
		return nil, parser.NoChildren
	}

	line, _ := reader.PeekLine()

	// allow blank lines before frontmatter
	if isBlank(line) {
		return nil, parser.NoChildren
	}

	if !bytes.HasPrefix(line, []byte("---")) {
		return nil, parser.NoChildren
	}

	// Do NOT consume the opening delimiter manually. Goldmark automatically
	// advances the reader past the triggering line when Open returns.
	return ast.NewTextBlock(), parser.NoChildren
}

func (p *frontmatterParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, seg := reader.PeekLine()
	if line == nil {
		return parser.Close
	}

	if bytes.HasPrefix(bytes.TrimSpace(line), []byte("---")) {
		// consume closing delimiter
		reader.Advance(seg.Len())

		// parse the collected yaml data
		source := reader.Source()
		tb := node.(*ast.TextBlock)
		var buf bytes.Buffer
		for i := 0; i < tb.Lines().Len(); i++ {
			s := tb.Lines().At(i)
			buf.Write(s.Value(source))
		}

		meta := defaultFrontmatter()
		if err := yaml.Unmarshal(buf.Bytes(), &meta); err == nil {
			meta.Macros = normalizeMacros(meta.Macros)
			pc.Set(metadataContextKey, meta)
		}

		return parser.Close
	}

	// Append line segment manually, but do NOT advance reader.
	// Goldmark automatically advances the reader to the next line.
	tb := node.(*ast.TextBlock)
	tb.Lines().Append(seg)
	return parser.Continue
}

func (p *frontmatterParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	// Clean up lines of the dummy node so it does not render in output
	tb := node.(*ast.TextBlock)
	tb.Lines().SetSliced(0, 0)
}

func (p *frontmatterParser) CanInterruptParagraph() bool {
	return true
}

func (p *frontmatterParser) CanAcceptIndentedLine() bool {
	return false
}

// IMPLEMENTATION OF KATEX EXTENSION
//

type Math struct {
	ast.BaseInline
	Segment text.Segment
	Display bool
}

func (n *Math) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"display": strconv.FormatBool(n.Display),
	}, nil)
}

var KindMath = ast.NewNodeKind("Math")

func (n *Math) Kind() ast.NodeKind {
	return KindMath
}

func NewMath(seg text.Segment, display bool) *Math {
	return &Math{
		Segment: seg,
		Display: display,
	}
}

type katexExtension struct{}

func (e *katexExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&mathParser{}, 500),
		),
	)

	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&mathRenderer{}, 500),
		),
	)
}

type mathParser struct{}

func (p *mathParser) Trigger() []byte {
	return []byte{'$'}
}

func (p *mathParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	_, seg := block.PeekLine()
	source := block.Source()
	pos := seg.Start

	if pos > 0 && source[pos-1] == '\\' {
		return nil
	}

	// determine delimiter length
	delim := 1
	if pos+1 < len(source) && source[pos+1] == '$' {
		delim = 2
	}

	start := pos + delim
	i := start

	for i < len(source) {
		if source[i] == '\\' {
			i += 2
			continue
		}

		if source[i] == '$' {
			if delim == 2 {
				if i+1 < len(source) && source[i+1] == '$' {
					content := text.NewSegment(start, i)
					block.Advance(i + 2 - pos)
					return NewMath(content, true)
				}
			} else {
				content := text.NewSegment(start, i)
				block.Advance(i + 1 - pos)
				return NewMath(content, false)
			}
		}

		i++
	}

	return nil
}

func (p *mathParser) CloseBlock(parent ast.Node, pc parser.Context) {}

type mathRenderer struct{}

func (r *mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMath, r.renderMath)
}

func (r *mathRenderer) renderMath(
	w util.BufWriter,
	source []byte,
	node ast.Node,
	entering bool,
) (ast.WalkStatus, error) {

	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*Math)
	content := n.Segment.Value(source)

	if n.Display {
		w.WriteString("$$")
		w.Write(content)
		w.WriteString("$$")
	} else {
		w.WriteByte('$')
		w.Write(content)
		w.WriteByte('$')
	}

	return ast.WalkSkipChildren, nil
}

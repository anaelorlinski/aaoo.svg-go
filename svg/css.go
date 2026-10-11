package svg

import (
	"strings"

	"github.com/anaelorlinski/aaoo.cssengine-go/cascade"
	pa "github.com/anaelorlinski/aaoo.cssengine-go/parser"
	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/selector"
	"github.com/anaelorlinski/aaoo.cssengine-go/stylesheet"
	"github.com/anaelorlinski/aaoo.cssengine-go/validation"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/logger"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Apply CSS to SVG documents.

// http://www.w3.org/TR/SVG/styling.html#StyleElement
// n has tag style. It returns the CSS of n, and its media attribute ("" for
// none).
func handleStyleElement(n *utils.HTMLNode) (css []byte, media string) {
	if n.DataAtom != atom.Style {
		return nil, ""
	}
	for _, v := range n.Attr {
		if v.Key == "type" && !isCSSType(v.Val) {
			return nil, ""
		}
		if v.Key == "media" {
			media = v.Val
		}
	}

	// extract the css
	return n.GetChildrenText(), media
}

// isCSSType reports whether the type attribute of a <style> is CSS: empty,
// or the text/css media type, ASCII case-insensitive, its parameters
// (charset) aside.
func isCSSType(typ string) bool {
	mediaType, _, _ := strings.Cut(typ, ";")
	mediaType = strings.TrimSpace(mediaType)
	return mediaType == "" || strings.EqualFold(mediaType, "text/css")
}

type declaration struct {
	property string
	value    string
}

// Parse declarations in a given rule content.
//
// Declarations naming a property this package registered with the CSS engine
// (see properties.go) are validated through it, and dropped with a warning
// when invalid — which is what an invalid declaration should do per CSS, and
// what SVG used to skip: an unparseable value was carried as a string and
// later became zero, silently. Everything else passes through as before:
// SVG's XML attributes (d, points, viewBox, cx…) were never CSS properties.
func parseDeclarations(input []pa.Token) (normalDeclarations, importantDeclarations []declaration) {
	for _, decl := range pa.ParseBlocksContents(input, false) {
		if decl, ok := decl.(pa.Declaration); ok {
			if strings.HasPrefix(string(decl.Name), "-") {
				continue
			}
			name := utils.AsciiLower(decl.Name)
			if !validateDeclaration(name, decl.Value) {
				continue
			}
			d := declaration{name, pa.Serialize(decl.Value)}
			if decl.Important {
				importantDeclarations = append(importantDeclarations, d)
			} else {
				normalDeclarations = append(normalDeclarations, d)
			}
		}
	}
	return normalDeclarations, importantDeclarations
}

// validateDeclaration reports whether the declaration should be kept. A
// property this package registered is checked by its validator; anything else
// is kept untouched.
func validateDeclaration(name string, value []pa.Token) bool {
	prop, isRegistered := pr.RegisteredProperty(name)
	if !isRegistered {
		return true
	}
	tokens := pa.RemoveWhitespace(value)
	if len(tokens) == 0 {
		return true
	}
	// CSS-wide keywords are handled by the cascade, not by the validator.
	if isCSSWideKeyword(pa.Serialize(tokens)) {
		return true
	}
	v, err := validation.ValidateKnown(prop, tokens, "")
	if err != nil || v == nil {
		logger.Warnf("ignoring invalid declaration %s: %s", name, pa.Serialize(value))
		return false
	}
	return true
}

type matcher = cascade.Matcher[declaration]

// Find stylesheets and return rule matchers. @import rules are fetched with
// urlFetcher; when it is nil they are ignored.
func parseStylesheets(stylesheets [][]byte, url string, urlFetcher Fetcher, device Device) (styleRules, stylesheet.Report) {
	var (
		out    styleRules
		report stylesheet.Report
		layers cascade.Layers // one layer order for the document
	)
	opts := stylesheet.Options{Layers: &layers, Device: device}
	if urlFetcher != nil {
		// there is deliberately no fallback fetcher: the caller's fetcher is
		// the policy deciding what may be read (local files, network hosts...)
		opts.Fetch = func(url string) ([]byte, string, error) {
			content, err := utils.FetchText(url, urlFetcher)
			if err != nil {
				return nil, "", err
			}
			return content, url, nil
		}
	}
	// the stylesheets of the <style> elements, walked in order: @import,
	// @media (against device), @supports and @layer follow the same rules
	// as in HTML
	for _, css := range stylesheets {
		r := stylesheet.Walk(pa.ParseStylesheetBytes(css, true, true), url, opts, &out)
		report.SizeDependent = report.SizeDependent || r.SizeDependent
		report.SizeQueries = append(report.SizeQueries, r.SizeQueries...)
	}
	out.layerRanks = layers.Ranks()
	out.cache = selector.NewCache()
	return out, report
}

// StyleRule adds a style rule to the matchers, split by importance.
func (out *styleRules) StyleRule(rule pa.QualifiedRule, layer int, _ string) bool {
	normalDeclarations, importantDeclarations := parseDeclarations(rule.Content)
	prelude := pa.Serialize(rule.Prelude)
	selector, err := selector.ParseGroup(prelude)
	if err != nil {
		logger.Warnf("Invalid or unsupported selector '%s', %s \n", prelude, err)
		return false
	}
	order := out.normal.Len() + out.important.Len()
	if len(normalDeclarations) != 0 {
		out.normal.Add(cascade.Rule[declaration]{Selectors: selector, Declarations: normalDeclarations, Order: order, Layer: layer})
	}
	if len(importantDeclarations) != 0 {
		out.important.Add(cascade.Rule[declaration]{Selectors: selector, Declarations: importantDeclarations, Order: order, Layer: layer})
	}
	return true
}

// AtRule: SVG has no at-rules of its own.
func (*styleRules) AtRule(pa.AtRule, int, string) bool { return false }

// styleRules are the style rules of an SVG document, split by importance,
// with the ranks of their cascade layers.
type styleRules struct {
	normal, important matcher
	layerRanks        []int
	// shared by the matches of the document, which does not change while
	// styled
	cache *selector.Cache
}

// replace `d` with the (potential) expanded properties
func expandProperty(d declaration) []declaration {
	if d.property != "font" {
		return []declaration{d}
	}

	tokens := pa.RemoveWhitespace(pa.Tokenize([]byte(d.value), true))
	expanded, err := validation.ExpandFont(tokens)
	if err != nil {
		logger.Warnf("ignoring %s property: %s", d.property, err)
		return nil
	}

	out := make([]declaration, len(expanded))
	for i, p := range expanded {
		out[i] = declaration{p[0], p[1]}
	}
	return out
}

func (attrs nodeAttributes) applyStyle(_ string, node *html.Node, rules *styleRules) {
	var normalAttr, importantAttr []declaration
	if styleAttr := attrs["style"]; styleAttr != "" {
		normalAttr, importantAttr = parseDeclarations(pa.Tokenize([]byte(styleAttr), false))
	}
	delete(attrs, "style") // not useful anymore

	// Cascade: presentation attributes (already in attrs) sit below all
	// CSS; matched rules apply in (layer, specificity, order), later layers
	// winning for normal declarations and earlier ones for !important; the
	// style attribute beats any selector of the same importance.
	var allProps []declaration
	// pseudo-elements (::before, ::marker, ...) do not exist in SVG: their
	// declarations never apply to the element itself
	normal := rules.normal.MatchWith(node, rules.cache)
	cascade.SortByLayer(normal, rules.layerRanks, false)
	for _, d := range normal {
		if d.PseudoElement == "" {
			allProps = append(allProps, d.Declaration)
		}
	}
	allProps = append(allProps, normalAttr...)
	important := rules.important.MatchWith(node, rules.cache)
	cascade.SortByLayer(important, rules.layerRanks, true)
	for _, d := range important {
		if d.PseudoElement == "" {
			allProps = append(allProps, d.Declaration)
		}
	}
	allProps = append(allProps, importantAttr...)
	for _, d := range allProps {
		expanded := expandProperty(d)
		for _, exp := range expanded {
			attrs[exp.property] = strings.TrimSpace(exp.value)
		}
	}
}

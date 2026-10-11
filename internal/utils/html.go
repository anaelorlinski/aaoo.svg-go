// Package utils holds the small helpers of the SVG library: walking the
// parsed document, joining URLs, fetching text.
package utils

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type HTMLNode html.Node

func (h *HTMLNode) AsHtmlNode() *html.Node {
	return (*html.Node)(h)
}

// Get returns the attribute `name` or ""
// See HasAttr if you need to distinguish between no attribute
// and an attribute with an empty string value.
func (h HTMLNode) Get(name string) string {
	for _, attr := range h.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

// HasAttr returns true if `name` is among the attributes (possibly empty).
func (h HTMLNode) HasAttr(name string) bool {
	for _, attr := range h.Attr {
		if attr.Key == name {
			return true
		}
	}
	return false
}

// Iter return an iterator over the html tree.
// If tags are given, only the node matching them
// will be returned by the iterator.
func (h *HTMLNode) Iter(tags ...atom.Atom) HTMLIterator {
	return NewHtmlIterator((*html.Node)(h), tags...)
}

// ------------------------------------ html walk utilities ------------------------------------

// HTMLIterator simplify the (depth first) walk on an HTML tree.
type HTMLIterator struct {
	tagsMap map[atom.Atom]bool // if nil, means all
	result  *html.Node         // valid element to return
	toVisit []*html.Node
}

// NewHtmlIterator use `root` as start point.
// If `tags` is given, only node matching one of them are returned.
func NewHtmlIterator(root *html.Node, tags ...atom.Atom) HTMLIterator {
	tagsMap := make(map[atom.Atom]bool)
	for _, tag := range tags {
		tagsMap[tag] = true
	}
	return HTMLIterator{toVisit: []*html.Node{root}, tagsMap: tagsMap}
}

// pop the stack
func (h *HTMLIterator) popNode() *html.Node {
	L := len(h.toVisit) - 1
	next := h.toVisit[L]
	h.toVisit = h.toVisit[:L]
	return next
}

// HasNext returns true if a node still has to be visited.
func (h *HTMLIterator) HasNext() bool {
	if h.result != nil { // empty the stack
		return true
	}

	if len(h.toVisit) == 0 { // walk is done
		return false
	}

	next := h.popNode()
	if next.NextSibling != nil {
		h.toVisit = append(h.toVisit, next.NextSibling)
	}
	if next.FirstChild != nil {
		h.toVisit = append(h.toVisit, next.FirstChild)
	}

	if len(h.tagsMap) == 0 || h.tagsMap[next.DataAtom] { // found one element
		h.result = next
		return true
	}

	// check the remaining nodes
	return h.HasNext()
}

func (h *HTMLIterator) Next() *HTMLNode {
	out := (*HTMLNode)(h.result)
	h.result = nil
	return out
}

// NodeChildren returns the direct children of `element`.
// Skip empty text nodes
func (element HTMLNode) NodeChildren(skipBlank bool) (children []*HTMLNode) {
	child := element.FirstChild
	for child != nil {
		if !(skipBlank && child.Type == html.TextNode && strings.TrimSpace(child.Data) == "") {
			children = append(children, (*HTMLNode)(child))
		}
		child = child.NextSibling
	}
	return
}

// IsText returns true if the node is a non empty text node.
func (element HTMLNode) IsText() (bool, string) {
	if text := element.Data; element.Type == html.TextNode && text != "" {
		return true, text
	}
	return false, ""
}

// GetChildrenText returns the text directly in the element, but not descendants.
// It's the concatenation of all children's TextNodes.
func (element HTMLNode) GetChildrenText() (content []byte) {
	if element.Type == html.TextNode {
		content = []byte(element.Data)
	}

	for _, child := range element.NodeChildren(false) {
		if child.Type == html.TextNode {
			content = append(content, child.Data...)
		}
	}
	return content
}

// GetText returns the content of the first text node child.
// Due to Go html.Parse() behavior, this method mimic Python xml.etree.text
// attribute.
func (element HTMLNode) GetText() string {
	if c := element.FirstChild; c != nil && c.Type == html.TextNode {
		return c.Data
	}
	return ""
}

// Transform (only) ASCII letters to lower case: A-Z is mapped to a-z.
// This is used for `ASCII case-insensitive
// <http://whatwg.org/C#ascii-case-insensitive>`_ matching.
// This is different from the strings.ToLower function
// which also affect non-ASCII characters, sometimes mapping them into the ASCII range:
//
//	keyword := "Bac\u212Aground"
//	strings.ToLower(keyword) == "background"
//	AsciiLower(keyword) != strings.ToLower(keyword)
//	AsciiLower(keyword) == "bac\u212Aground"
func AsciiLower(s string) string {
	rs := []rune(s)
	out := make([]rune, len(rs))
	for index, c := range rs {
		if c < unicode.MaxASCII {
			c = unicode.ToLower(c)
		}
		out[index] = c
	}
	return string(out)
}

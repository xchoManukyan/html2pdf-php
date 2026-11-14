package htmlparse

import "testing"

func parentTag(n *Node, root *Node) string {
	var walk func(p *Node) string
	walk = func(p *Node) string {
		for _, c := range p.Children {
			if c == n {
				return p.Tag
			}
			if t := walk(c); t != "" {
				return t
			}
		}
		return ""
	}
	return walk(root)
}

func TestUnclosedHeadEndsAtBody(t *testing.T) {
	cases := map[string]string{
		"missing </head>": `<html><head><meta charset="utf-8"><link rel="stylesheet" href="a.css">
			<body><div class="x">Hello</div></body></html>`,
		"content in head": `<html><head><title>T</title><div class="x">Hello</div></html>`,
		"text in head":    `<head><style>p{}</style>Hello <div class="x">there</div>`,
	}
	for name, src := range cases {
		root := Parse(src)
		div := root.Find("div")
		if div == nil {
			t.Fatalf("%s: div not found", name)
		}
		if p := parentTag(div, root); p == "head" {
			t.Errorf("%s: body content must not stay inside <head>", name)
		}
		if head := root.Find("head"); head != nil && head.Find("div") != nil {
			t.Errorf("%s: <head> swallowed the content", name)
		}
	}
	// head elements stay in the head, and <title>/<style> text does not end it
	root := Parse(`<head><title>T</title><style>p{color:red}</style><link rel="x"></head><body><p>x</p>`)
	head := root.Find("head")
	if head == nil || head.Find("link") == nil || head.Find("style") == nil {
		t.Error("<link> and <style> should remain in <head>")
	}
}

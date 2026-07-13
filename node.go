package main

import (
	"fmt"
	"io"
	"strings"
)

// node is one entry in the execution trace tree.
type node struct {
	// pos is the file:line this entry points at, printed in the left
	// gutter; empty for annotation lines with no location of their own.
	pos  string
	text string
	// structural nodes (loops, branches, select) are pruned when they end
	// up with no children, since they carry no calls or channel activity.
	structural bool
	// loop nodes carry a "%d" placeholder in text; numbers are assigned
	// after pruning so they stay contiguous.
	loop bool
	kids []*node
}

func (n *node) add(k *node) *node {
	n.kids = append(n.kids, k)
	return k
}

func (n *node) addf(format string, args ...any) *node {
	return n.add(&node{text: fmt.Sprintf(format, args...)})
}

// addp adds a child with a source position for the left gutter.
func (n *node) addp(pos, format string, args ...any) *node {
	return n.add(&node{pos: pos, text: fmt.Sprintf(format, args...)})
}

// prune removes structural nodes that contain no trace events.
func prune(n *node) {
	kept := n.kids[:0]
	for _, k := range n.kids {
		prune(k)
		if k.structural && len(k.kids) == 0 {
			continue
		}
		kept = append(kept, k)
	}
	n.kids = kept
}

// numberLoops assigns loop numbers in trace order (depth-first).
func numberLoops(n *node, counter *int) {
	if n.loop {
		*counter++
		n.text = fmt.Sprintf(n.text, *counter)
	}
	for _, k := range n.kids {
		numberLoops(k, counter)
	}
}

// render prints the tree with file:line in a fixed-width left gutter and
// the trace itself indented to the right.
func render(w io.Writer, root *node) {
	width := 0
	measure(root, &width)
	renderAt(w, root, 0, width)
}

func measure(n *node, width *int) {
	if len(n.pos) > *width {
		*width = len(n.pos)
	}
	for _, k := range n.kids {
		measure(k, width)
	}
}

func renderAt(w io.Writer, n *node, depth, width int) {
	fmt.Fprintf(w, "%-*s │ %s%s\n", width, n.pos, strings.Repeat("  ", depth), n.text)
	for _, k := range n.kids {
		renderAt(w, k, depth+1, width)
	}
}

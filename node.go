package main

import (
	"fmt"
	"strings"
)

// span is one segment of a node's text. Segments with V != 0 are variable
// occurrences: V is the variable's alias-class ID, stable across the whole
// trace, so the UI can color and track a variable through argument passing,
// assignments and returns without doing any analysis itself.
type span struct {
	T string `json:"t"`
	V int    `json:"v,omitempty"`
	// R marks a returned variable (rendered with a red marker in the UI while
	// keeping its alias-class color, so it stays trackable).
	R bool `json:"r,omitempty"`
}

func spansText(spans []span) string {
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.T)
	}
	return sb.String()
}

// node is one entry in the execution trace tree. It serializes directly to
// JSON for the UI, which renders it without further interpretation — every
// semantic decision (kind, label, position) is made here in the backend.
type node struct {
	// Pos is the file:line this entry points at; empty for lines with no
	// location of their own.
	Pos string `json:"pos,omitempty"`
	// Kind tells the UI what this entry is:
	//   root | call | interface-call | func-value-call | indirect-call |
	//   impl | bound | go | defer | loop | branch | case | select |
	//   chan-send | chan-recv | chan-close | peer | arg | note
	Kind string `json:"kind,omitempty"`
	// Label classifies a callee: local | stdlib | module | builtin.
	Label string `json:"label,omitempty"`
	// Num is the loop number (kind "loop"), assigned after pruning.
	Num  int     `json:"num,omitempty"`
	Text string  `json:"text"`
	Kids []*node `json:"kids,omitempty"`
	// Spans is Text split into segments with variable occurrences marked;
	// when present, Text is the plain concatenation of the segments.
	Spans []span `json:"spans,omitempty"`

	// structural nodes (loops, branches, select) are pruned when they end
	// up with no children, since they carry no calls or channel activity.
	structural bool
	loop       bool
}

func (n *node) add(k *node) *node {
	n.Kids = append(n.Kids, k)
	return k
}

// nodeWithSpans builds a node whose text carries variable markers.
func nodeWithSpans(pos, kind, label string, spans []span) *node {
	return &node{Pos: pos, Kind: kind, Label: label, Spans: spans, Text: spansText(spans)}
}

// note adds a plain informational child without a source position.
func (n *node) note(format string, args ...any) *node {
	return n.add(&node{Kind: "note", Text: fmt.Sprintf(format, args...)})
}

// notep adds an informational child pointing at a source position.
func (n *node) notep(pos, format string, args ...any) *node {
	return n.add(&node{Kind: "note", Pos: pos, Text: fmt.Sprintf(format, args...)})
}

// prune removes structural nodes that contain no trace events.
func prune(n *node) {
	kept := n.Kids[:0]
	for _, k := range n.Kids {
		prune(k)
		if k.structural && len(k.Kids) == 0 {
			continue
		}
		kept = append(kept, k)
	}
	n.Kids = kept
}

// numberLoops assigns loop numbers in trace order (depth-first).
func numberLoops(n *node, counter *int) {
	if n.loop {
		*counter++
		n.Num = *counter
	}
	for _, k := range n.Kids {
		numberLoops(k, counter)
	}
}

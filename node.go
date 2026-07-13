package main

import "fmt"

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

	// structural nodes (loops, branches, select) are pruned when they end
	// up with no children, since they carry no calls or channel activity.
	structural bool
	loop       bool
}

func (n *node) add(k *node) *node {
	n.Kids = append(n.Kids, k)
	return k
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

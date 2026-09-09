// Package window defines the workspace model of the multiplexer: the layout
// tree primitives (Node, SplitDir) shared by the layout algorithms, the Window
// save-slot type, and the Registry that tracks the ordered list of windows.
//
// A layout tree is made of Nodes: a leaf holds one Pane, a container holds
// children arranged in one SplitDir. Each window owns one tree; only the active
// window's tree is manipulated at any time, and it is mirrored into a Window
// save slot so the Registry can switch between workspaces.
package window

import "github.com/atharva-0123-cs/tatami/pane"

// SplitDir represents the direction of a split.
type SplitDir int

const (
	SplitHorizontal SplitDir = iota // children laid out top-to-bottom
	SplitVertical                   // children laid out left-to-right
)

// Node is a node in the layout tree. A leaf holds a single Pane; a container
// holds children arranged in one direction.
//
// Sibling rectangles within one container partition the container's rectangle
// proportionally to their sizes (see layout's seamSpans). Each leaf insets its
// assigned rectangle by the window's gap inset on every side when drawing, so
// adjacent panes keep a thin visible gutter instead of touching borders. Leaf
// nodes have a nil Children/Sizes slice; container nodes have a nil Pane.
type Node struct {
	Leaf   bool
	Pane   *pane.Pane // non-nil only if Leaf
	Dir    SplitDir   // meaningful only if !Leaf
	Parent *Node

	Children []*Node
	Sizes    []int // proportional sizes for flex layout
}

// AddChild appends a child to a container node.
//
// The child records this node as its Parent and receives the given
// proportional flex size; the existing sizes are left unchanged.
func (n *Node) AddChild(child *Node, size int) {
	child.Parent = n
	n.Children = append(n.Children, child)
	n.Sizes = append(n.Sizes, size)
}

// RemoveChild removes a child from a container node.
//
// The child's Parent is cleared, and both the children and matching size
// entries are removed. If child is not present the node is unchanged.
func (n *Node) RemoveChild(child *Node) {
	for i, c := range n.Children {
		if c == child {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			n.Sizes = append(n.Sizes[:i], n.Sizes[i+1:]...)
			child.Parent = nil
			return
		}
	}
}

// ReplaceChild replaces oldChild with newChild in the parent container.
//
// The new child adopts the container as its Parent; oldChild is detached. The
// proportional sizes are kept as-is (the replacement inherits the old slot's
// weight). If oldChild is not found the tree is unchanged.
func (n *Node) ReplaceChild(oldChild, newChild *Node) {
	for i, c := range n.Children {
		if c == oldChild {
			n.Children[i] = newChild
			newChild.Parent = n
			oldChild.Parent = nil
			return
		}
	}
}

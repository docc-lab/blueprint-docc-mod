package leaf

import "context"

// TreeNode is a synthetic reverse-truss test node. It forwards a call to each of
// its children. Topology (leaf/unary/binary) is chosen per instance in the wiring
// spec. Leaves are terminal, so flag them with RT_LEAF_REJECT.
type TreeNode interface {
	Process(ctx context.Context, n int64) (int64, error)
}

// Leaf: 0 children (terminal). LeafReject runs here.
type LeafNodeImpl struct{ TreeNode }

func NewLeafNodeImpl(ctx context.Context) (*LeafNodeImpl, error) { return &LeafNodeImpl{}, nil }
func (s *LeafNodeImpl) Process(ctx context.Context, n int64) (int64, error) { return n + 1, nil }

// Unary: 1 child
type UnaryNodeImpl struct {
	TreeNode
	child TreeNode
}

func NewUnaryNodeImpl(ctx context.Context, child TreeNode) (*UnaryNodeImpl, error) {
	return &UnaryNodeImpl{child: child}, nil
}
func (s *UnaryNodeImpl) Process(ctx context.Context, n int64) (int64, error) {
	return s.child.Process(ctx, n)
}

// Binary: 2 children, so fan-in point (each child call returns a retCtx; on
// push-up they get merged/concatenated)
type BinaryNodeImpl struct {
	TreeNode
	left  TreeNode
	right TreeNode
}

func NewBinaryNodeImpl(ctx context.Context, left TreeNode, right TreeNode) (*BinaryNodeImpl, error) {
	return &BinaryNodeImpl{left: left, right: right}, nil
}
func (s *BinaryNodeImpl) Process(ctx context.Context, n int64) (int64, error) {
	a, err := s.left.Process(ctx, n)
	if err != nil {
		return 0, err
	}
	b, err := s.right.Process(ctx, n)
	if err != nil {
		return 0, err
	}
	return a + b, nil
}
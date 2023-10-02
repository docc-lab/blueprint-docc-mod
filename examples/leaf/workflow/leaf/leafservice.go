package leaf

import (
	ctxx "context"
	"fmt"

	"gitlab.mpi-sws.org/cld/blueprint/runtime/core/backend"
)

type MyInt int64

type NestedLeafObject struct {
	Key   string
	Value string
	Props []string
}

type LeafObject struct {
	ID    int64
	Name  string
	Props map[string]NestedLeafObject
}

type LeafService interface {
	HelloNothing(ctx ctxx.Context) error
	HelloInt(ctx ctxx.Context, a int64) (int64, error)
	HelloObject(ctx ctxx.Context, obj *LeafObject) (*LeafObject, error)
	HelloMate(ctx ctxx.Context, a int, b int32, c string, d map[string]LeafObject, elems []string, elems2 []NestedLeafObject) (string, []string, int32, int, map[string]LeafObject, error)
}

type LeafServiceImpl struct {
	LeafService
	Cache backend.Cache
}

func (l *LeafServiceImpl) HelloNothing(ctx ctxx.Context) error {
	fmt.Println("hello nothing!")
	return nil
}

func (l *LeafServiceImpl) HelloInt(ctx ctxx.Context, a int64) (int64, error) {
	fmt.Println("hello")
	l.Cache.Put(ctx, "helloint", a)
	var b int64
	l.Cache.Get(ctx, "helloint", &b)
	return b, nil
}

func (l *LeafServiceImpl) HelloObject(ctx ctxx.Context, obj *LeafObject) (*LeafObject, error) {
	return obj, nil
}

func (l *LeafServiceImpl) HelloMate(ctx ctxx.Context, a int, b int32, c string, d map[string]LeafObject, elems []string, elems2 []NestedLeafObject) (string, []string, int32, int, map[string]LeafObject, error) {
	return c, elems, b, a, d, nil
}

func (l *LeafServiceImpl) NonServiceFunction() int64 {
	return 3
}

func NewLeafServiceImpl(cache backend.Cache) (*LeafServiceImpl, error) {
	return &LeafServiceImpl{Cache: cache}, nil
}

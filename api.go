package godi

import (
	"context"
	"fmt"
	"reflect"
)

// Resolver is the resolution half of Provider and Scope. The generic helpers
// (Resolve, ResolveKeyed, ResolveGroup and their Must variants) accept any
// Resolver, so code that only resolves services can depend on this narrower
// interface — and tests can supply a small fake.
type Resolver interface {
	Get(serviceType reflect.Type) (any, error)
	GetKeyed(serviceType reflect.Type, key any) (any, error)
	GetGroup(serviceType reflect.Type, group string) ([]any, error)
}

// ResolveFromContext resolves a service of type T from the scope stored in
// ctx (see FromContext), as HTTP handlers under a godi scope middleware do:
//
//	svc, err := godi.ResolveFromContext[*UserService](r.Context())
func ResolveFromContext[T any](ctx context.Context) (T, error) {
	scope, err := FromContext(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return Resolve[T](scope)
}

// Instance marks v as a pre-built value to register, not a constructor. Pass
// it to AddSingleton when the value itself is a function, which would
// otherwise be taken for a constructor:
//
//	type Clock func() time.Time
//	services.AddSingleton(godi.Instance(Clock(time.Now)))
//
// Other values can be passed to AddSingleton directly.
func Instance(v any) any {
	return instanceValue{value: v}
}

// instanceValue wraps a value registered through Instance.
type instanceValue struct{ value any }

// Key is an AddOption that registers the service under key, which may be any
// comparable value (a string, an enum constant, a struct). Resolve it with
// ResolveKeyed or GetKeyed using an equal key. godi.Name(s) is the same as
// godi.Key(s) for a string s; struct tags (name:"...") can refer to string
// keys only.
func Key(key any) AddOption {
	return addKeyOption{key: key}
}

type addKeyOption struct{ key any }

func (o addKeyOption) String() string { return fmt.Sprintf("Key(%v)", o.key) }

func (o addKeyOption) applyAddOption(opt *addOptions) {
	opt.Key = o.key
}

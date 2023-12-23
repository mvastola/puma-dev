package rpc

import (
	"fmt"
	"github.com/carlmjohnson/truthy"
	"log"
	"reflect"
	"sync"
)

type MaybeIface[T any] interface {
	HasValue() bool
	Clear()
	Set(T) bool
	ApplyDefault(...any) bool
	ValueOr(T) T
	Ptr() *T
	Value() T
	WithValue(fn func(T) any, fallback any) any
}

type Maybe[T any] struct {
	hasValue bool
	value    T
	mtx      *sync.Mutex
}

func NewMaybe[T any](value ...T) Maybe[T] {
	result := Maybe[T]{hasValue: false, mtx: new(sync.Mutex)}
	if len(value) > 1 {
		log.Fatalf("optional.New[T]() accepts at most 1 argument")
	} else if len(value) == 1 {
		result.Set(value[0])
	}
	return result
}

func (m *Maybe[T]) HasValue() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m != nil && m.hasValue
}

func (m *Maybe[T]) Clear() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.hasValue = false
	var newVal T
	m.value = newVal
}

func (m *Maybe[T]) Set(newValue T) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.hasValue = truthy.ValueAny(newValue)
	if m.hasValue {
		m.value = newValue
	}
	return m.hasValue
}

func (m *Maybe[T]) ApplyDefault(values ...interface{}) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.hasValue {
		return true
	}
	maybePtrType := reflect.TypeOf(m)
	maybeType := reflect.TypeOf(*m)
	genericType := reflect.TypeOf(m.value)
	genericPtrType := reflect.TypeOf(&m.value)
	var maybeValue Maybe[T] = NewMaybe[T]()

	for _, value := range values {
		valueType := reflect.TypeOf(value)
		valuePtrType := reflect.TypeOf(&value)

		if valueType.AssignableTo(maybePtrType) {
			maybePtr, ok := value.(*Maybe[T])
			if ok {
				maybeValue = *maybePtr
			}
		} else if valuePtrType.AssignableTo(maybeType) {
			maybeVal, ok := value.(Maybe[T])
			if ok {
				maybeValue = maybeVal
			}
		} else if valueType.AssignableTo(genericType) {
			maybeValue.Set(value.(T))
		} else if valuePtrType.AssignableTo(genericPtrType) {
			maybeValue.Set(*value.(*T))
		}

		if maybeValue.HasValue() {
			m.value = maybeValue.value
			m.hasValue = true
			return true
		}
	}
	return false
}

func (m *Maybe[T]) ValueOr(fallback T) T {
	if m != nil && m.hasValue {
		return m.value
	}
	return fallback
}

func (m *Maybe[T]) Ptr() *T {
	if m == nil {
		return nil
	}
	return &m.value
}

func (m *Maybe[T]) Value() T {
	if !m.HasValue() {
		_ = fmt.Errorf("cannot call Value() on unset MaybeIface object: %v", m)
	}
	return *m.Ptr()
}

func (m *Maybe[T]) WithValue(fn any, rest ...any) any {
	fnType := reflect.TypeOf(fn)
	tType := reflect.TypeOf([0]T{}).Elem()
	if fnType.Kind() != reflect.Func {
		panic("Maybe[T].WithValue must be called with a function as its first argument")
	} else if fnType.NumIn() != 1 {
		panic("Function passed to Maybe[T].WithValue must take exactly one argument")
	}
	fnArgType := fnType.In(0)
	if !tType.AssignableTo(fnType.In(0)) {
		log.Fatalf(
			"Cannot call fn %v with argument of type %s (requires %s)\n",
			fn,
			tType.String(),
			fnArgType.String(),
		)
	}
	var fallback any = nil
	if len(rest) > 1 {
		panic("Maybe[T].WithValue can take at most one fallback argument")
	} else if len(rest) == 1 {
		fallback = rest[0]
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()
	if !m.hasValue {
		return fallback
	}
	var resultValues []reflect.Value
	fnValue := reflect.ValueOf(fn)
	resultValues = fnValue.Call([]reflect.Value{reflect.ValueOf(m.value)})
	var resultIfaces []interface{} = make([]interface{}, len(resultValues))

	for idx, res := range resultValues {
		resultIfaces[idx] = res.Interface()
	}
	return resultIfaces
}

package manage

import (
	"context"
	"fmt"
	"reflect"
)

type descriptor struct {
	newModel    func() any
	idField     string
	afterUpsert func(context.Context, *Wrapper, any) error
	afterDelete func(context.Context, *Wrapper, any) error
}

var descriptors = map[string]descriptor{}

func lookupDescriptor(resource string) (descriptor, error) {
	desc, ok := descriptors[resource]
	if !ok {
		return descriptor{}, fmt.Errorf("unsupported resource %q", resource)
	}
	return desc, nil
}

func newSlicePtr(newModel func() any) any {
	modelType := reflect.TypeOf(newModel())
	sliceType := reflect.SliceOf(modelType.Elem())
	return reflect.New(sliceType).Interface()
}

func derefValue(ptr any) any {
	return reflect.ValueOf(ptr).Elem().Interface()
}

// Package protocolmeta owns the private AG-UI envelope stored in Eino Extra
// fields and the cloning rules used at that boundary.
package protocolmeta

import (
	"reflect"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

// ExtraKey namespaces AG-UI-only values in Eino Extra maps.
const ExtraKey = "github.com/mattsp1290/eino-agui/ag-ui"

// CloneMetadata recursively clones maps, slices, arrays, pointers, and
// interfaces while preserving named Go container types.
func CloneMetadata(in aguitypes.Metadata) aguitypes.Metadata {
	if in == nil {
		return nil
	}
	out := make(aguitypes.Metadata, len(in))
	for key, value := range in {
		cloned := cloneValue(reflect.ValueOf(value))
		if cloned.IsValid() {
			out[key] = cloned.Interface()
		} else {
			out[key] = nil
		}
	}
	return out
}

// CloneMap recursively clones a string-keyed Extra map.
func CloneMap(in map[string]any) map[string]any {
	if in == nil {
		return make(map[string]any)
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		cloned := cloneValue(reflect.ValueOf(value))
		if cloned.IsValid() {
			out[key] = cloned.Interface()
		} else {
			out[key] = nil
		}
	}
	return out
}

func cloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneValue(value.Elem()))
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneValue(value.Elem()))
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneValue(iter.Value()))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneValue(value.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneValue(value.Index(i)))
		}
		return out
	default:
		return value
	}
}

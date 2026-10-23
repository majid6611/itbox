package api

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// emptyLists is a response transformer that turns every nil slice in a
// response body into an empty one. Go encodes a nil slice as JSON null, and
// the pages read `.length` on lists — one null crashes the whole page. Doing
// it here covers every endpoint, including ones added later.
func emptyLists(_ huma.Context, _ string, v any) (any, error) {
	if v == nil {
		return v, nil
	}
	return fixNil(reflect.ValueOf(v)).Interface(), nil
}

// fixNil returns a copy of v with nil slices replaced by empty ones. It
// copies rather than editing in place, since response bodies are often
// struct values that can't be modified.
func fixNil(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return v // []byte: leave alone
			}
			return reflect.MakeSlice(v.Type(), 0, 0)
		}
		if !needsFix(v.Type().Elem()) {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(fixNil(v.Index(i)))
		}
		return out
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(fixNil(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(fixNil(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() || !needsFix(v.Type().Elem()) {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), fixNil(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if f := out.Field(i); f.CanSet() && needsFix(f.Type()) {
				f.Set(fixNil(v.Field(i)))
			}
		}
		return out
	}
	return v
}

func needsFix(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice:
		return true
	case reflect.Ptr, reflect.Map:
		return needsFix(t.Elem())
	case reflect.Interface:
		return true
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && needsFix(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

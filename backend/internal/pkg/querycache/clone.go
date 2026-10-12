package querycache

import (
	"maps"
	"reflect"
)

// ShallowMap 复制顶层映射条目，嵌套值与输入共享。
// nil 输入返回已分配的空映射。
func ShallowMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	maps.Copy(out, values)
	return out
}

// Clone 复制值中的 map、slice 和指针。不可变值的未导出字段保持原值。
func Clone[T any](value T) T {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return value
	}
	result, ok := reflect.TypeAssert[T](cloneValue(v))
	if !ok {
		panic("querycache: cloned value changed type")
	}
	return result
}

// cloneValue 递归复制可变值及结构体的导出字段。
func cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneValue(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneValue(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			out.SetMapIndex(it.Key(), cloneValue(it.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(cloneValue(v.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(cloneValue(v.Field(i)))
			}
		}
		return out
	default:
		return v
	}
}

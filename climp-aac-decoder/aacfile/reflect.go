package aacfile

import (
	"reflect"
	"unsafe"
)

func exposeValue(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	if v.CanAddr() {
		return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	}
	return v
}

func fieldValue(obj any, name string) reflect.Value {
	if obj == nil {
		return reflect.Value{}
	}

	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		return reflect.Value{}
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = exposeValue(v.Elem())
	} else {
		v = exposeValue(v)
	}

	f := v.FieldByName(name)
	if !f.IsValid() {
		return reflect.Value{}
	}
	return exposeValue(f)
}

func uint8Field(obj any, name string) uint8 {
	v := fieldValue(obj, name)
	if !v.IsValid() {
		return 0
	}
	return uint8(v.Uint())
}

func boolField(obj any, name string) bool {
	v := fieldValue(obj, name)
	if !v.IsValid() {
		return false
	}
	return v.Bool()
}

func fieldAny(obj any, name string) any {
	v := fieldValue(obj, name)
	if !v.IsValid() {
		return nil
	}
	return v.Interface()
}

func uint8Slice(v reflect.Value) []uint8 {
	if !v.IsValid() {
		return nil
	}
	out := make([]uint8, v.Len())
	for i := range out {
		out[i] = uint8(exposeValue(v.Index(i)).Uint())
	}
	return out
}

func uint8Matrix(v reflect.Value) [][]uint8 {
	if !v.IsValid() {
		return nil
	}
	out := make([][]uint8, v.Len())
	for i := range out {
		out[i] = uint8Slice(exposeValue(v.Index(i)))
	}
	return out
}

func uint8Cube(v reflect.Value) [][][]uint8 {
	if !v.IsValid() {
		return nil
	}
	out := make([][][]uint8, v.Len())
	for i := range out {
		m := exposeValue(v.Index(i))
		out[i] = make([][]uint8, m.Len())
		for j := range out[i] {
			out[i][j] = uint8Slice(exposeValue(m.Index(j)))
		}
	}
	return out
}

func uint16Slice(v reflect.Value) []int {
	if !v.IsValid() {
		return nil
	}
	out := make([]int, v.Len())
	for i := range out {
		out[i] = int(exposeValue(v.Index(i)).Uint())
	}
	return out
}

func uint16Matrix(v reflect.Value) [][]int {
	if !v.IsValid() {
		return nil
	}
	out := make([][]int, v.Len())
	for i := range out {
		out[i] = uint16Slice(exposeValue(v.Index(i)))
	}
	return out
}

func intSlice(v reflect.Value) []int {
	if !v.IsValid() {
		return nil
	}
	out := make([]int, v.Len())
	for i := range out {
		out[i] = numericValue(exposeValue(v.Index(i)))
	}
	return out
}

func int8Matrix(v reflect.Value) [][]int {
	if !v.IsValid() {
		return nil
	}
	out := make([][]int, v.Len())
	for i := range out {
		row := exposeValue(v.Index(i))
		values := make([]int, row.Len())
		for j := range values {
			values[j] = int(exposeValue(row.Index(j)).Int())
		}
		out[i] = values
	}
	return out
}

func boolMatrix(v reflect.Value) [][]bool {
	if !v.IsValid() {
		return nil
	}
	out := make([][]bool, v.Len())
	for i := range out {
		row := exposeValue(v.Index(i))
		values := make([]bool, row.Len())
		for j := range values {
			values[j] = exposeValue(row.Index(j)).Bool()
		}
		out[i] = values
	}
	return out
}

func numericValue(v reflect.Value) int {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(v.Uint())
	default:
		return 0
	}
}

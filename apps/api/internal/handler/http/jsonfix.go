package httpapi

import "reflect"

// withEmptySlices returns a deep copy of v in which every nil slice is replaced by
// an empty one, so clients receive [] where the OpenAPI contract (generated from
// these Go types) declares an array, instead of null. []byte and json.RawMessage
// are left untouched. The input is never modified, so shared data (e.g. package
// level catalogs) stays race-free.
func withEmptySlices(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	cp := reflect.New(rv.Type()).Elem()
	cp.Set(rv)
	fillEmptySlices(cp, 0)
	return cp.Interface()
}

// fillEmptySlices rewrites v (which must be settable) in place; pointers, slices,
// maps and interfaces are re-allocated before descending so nothing shared is touched.
func fillEmptySlices(v reflect.Value, depth int) {
	if depth > 64 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		np := reflect.New(v.Type().Elem())
		np.Elem().Set(v.Elem())
		fillEmptySlices(np.Elem(), depth+1)
		v.Set(np)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := v.Elem()
		cp := reflect.New(inner.Type()).Elem()
		cp.Set(inner)
		fillEmptySlices(cp, depth+1)
		v.Set(cp)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).IsExported() {
				fillEmptySlices(v.Field(i), depth+1)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return // []byte, json.RawMessage
		}
		if v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			return
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(cp, v)
		for i := 0; i < cp.Len(); i++ {
			fillEmptySlices(cp.Index(i), depth+1)
		}
		v.Set(cp)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return // uuid.UUID and other byte arrays
		}
		for i := 0; i < v.Len(); i++ {
			fillEmptySlices(v.Index(i), depth+1)
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		nm := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			val := reflect.New(iter.Value().Type()).Elem()
			val.Set(iter.Value())
			fillEmptySlices(val, depth+1)
			nm.SetMapIndex(iter.Key(), val)
		}
		v.Set(nm)
	}
}

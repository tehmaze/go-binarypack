package binarypack

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

func Marshal(v ...interface{}) ([]byte, error) {
	e := new(encodeState)
	e.s0, e.s1, e.s2, e.s4, e.s8 = e.scratch[:0], e.scratch[:1], e.scratch[:2], e.scratch[:4], e.scratch[:8]
	for _, vv := range v {
		if err := e.marshal(vv); err != nil {
			return nil, err
		}
	}
	return e.Bytes(), nil
}

// Marshaler is the interface implemented by objects that can newPtrEncoder(t reflect.Type) encoderFunc {/arshal
// themselves into valid BinaryPack.
type Marshaler interface {
	MarshalBinaryPack() ([]byte, error)
}

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "binarypack: unsupported type: " + e.Type.String()
}

type UnsupportedValueError struct {
	Value  reflect.Value
	String string
}

func (e *UnsupportedValueError) Error() string {
	return "binarypack: unsupported value: " + e.String
}

type MarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *MarshalerError) Error() string {
	return "binarypack: error calling MarshalBinaryPack for type " + e.Type.String() + ": " + e.Err.Error()
}

type encodeState struct {
	bytes.Buffer
	scratch            [64]byte
	s0, s1, s2, s4, s8 []byte
}

var encodeStatePool sync.Pool

func newEncodeState() *encodeState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*encodeState)
		e.Reset()
		return e
	}
	return new(encodeState)
}

func (e *encodeState) marshal(v interface{}) (err error) {
	defer func() {
		return
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(r)
			}
			if s, ok := r.(string); ok {
				panic(s)
			}
			err = r.(error)
		}
	}()
	e.reflectValue(reflect.ValueOf(v))
	return
}

func (e *encodeState) error(err error) {
	panic(err)
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		return v.Int() == 0
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

func (e *encodeState) reflectValue(v reflect.Value) {
	valueEncoder(v)(e, v)
}

func (e *encodeState) rawBytes(r []byte) int {
	p := e.Len()
	l := len(r)
	switch {
	case l < 16:
		e.WriteByte(0xa0 | byte(l))
	case l < math.MaxUint16:
		e.WriteByte(0xda)
		e.writeUint16(uint16(l))
	case l < math.MaxUint32:
		e.WriteByte(0xdb)
		e.writeUint32(uint32(l))
	}
	e.Write(r)
	return e.Len() - p
}

func (e *encodeState) stringBytes(s []byte) int {
	p := e.Len()
	l := len(s)
	switch {
	case l < 16:
		e.WriteByte(0xb0 | byte(l))
	case l < math.MaxUint16:
		u := uint16(l)
		e.WriteByte(0xd8)
		e.WriteByte(byte(u >> 8))
		e.WriteByte(byte(u))
	case l < math.MaxUint32:
		e.WriteByte(0xd9)
		e.writeUint32(uint32(l))
	}
	e.Write(s)
	return e.Len() - p
}

func (e *encodeState) writeUint16(u uint16) {
	e.Write([]byte{
		byte(u >> 8),
		byte(u),
	})
}

func (e *encodeState) writeUint32(u uint32) {
	e.Write([]byte{
		byte(u >> 24),
		byte(u >> 16),
		byte(u >> 8),
		byte(u),
	})
}

func (e *encodeState) writeUint64(u uint64) {
	e.Write([]byte{
		byte(u >> 56),
		byte(u >> 48),
		byte(u >> 40),
		byte(u >> 32),
		byte(u >> 24),
		byte(u >> 16),
		byte(u >> 8),
		byte(u),
	})
}

type encoderFunc func(e *encodeState, v reflect.Value)

var encoderCache struct {
	sync.RWMutex
	f map[reflect.Type]encoderFunc
}

func valueEncoder(v reflect.Value) encoderFunc {
	if !v.IsValid() {
		return invalidValueEncoder
	}
	return typeEncoder(v.Type())
}

func typeEncoder(t reflect.Type) encoderFunc {
	encoderCache.RLock()
	f := encoderCache.f[t]
	encoderCache.RUnlock()
	if f != nil {
		return f
	}

	encoderCache.Lock()
	if encoderCache.f == nil {
		encoderCache.f = make(map[reflect.Type]encoderFunc)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	encoderCache.f[t] = func(e *encodeState, v reflect.Value) {
		wg.Wait()
		f(e, v)
	}
	encoderCache.Unlock()

	// Compute fields without lock
	f = newTypeEncoder(t, true)
	wg.Done()
	encoderCache.Lock()
	encoderCache.f[t] = f
	encoderCache.Unlock()
	return f
}

var (
	marshalerType     = reflect.TypeOf(new(Marshaler)).Elem()
	textMarshalerType = reflect.TypeOf(new(encoding.TextMarshaler)).Elem()
)

func newTypeEncoder(t reflect.Type, allowAddr bool) encoderFunc {
	if t.Implements(marshalerType) {
		return marshalEncoder
	}
	if t.Kind() != reflect.Ptr && allowAddr {
		if reflect.PtrTo(t).Implements(marshalerType) {
			return newCondAddrEncoder(addrMarshalerEncoder, newTypeEncoder(t, false))
		}
	}
	if t.Implements(textMarshalerType) {
		return textMarshalerEncoder
	}
	if t.Kind() != reflect.Ptr && allowAddr {
		if reflect.PtrTo(t).Implements(textMarshalerType) {
			return newCondAddrEncoder(addrTextMarshalerEncoder, newTypeEncoder(t, false))
		}
	}

	switch t.Kind() {
	case reflect.Bool:
		return boolEncoder
	case reflect.Float32:
		return floatEncoder
	case reflect.Float64:
		return doubleEncoder
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		return intEncoder
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		return uintEncoder
	case reflect.String:
		return stringEncoder
	case reflect.Map:
		return mapEncoder
	case reflect.Slice:
		return sliceEncoder
	case reflect.Struct:
		return newStructEncoder(t)
	case reflect.Ptr:
		return newPtrEncoder(t)
	default:
		return unsupportedTypeEncoder
	}
}

func unsupportedTypeEncoder(e *encodeState, v reflect.Value) {
	e.error(&UnsupportedTypeError{v.Type()})
}

type condAddrEncoder struct {
	canAddrEnc, elseEnc encoderFunc
}

func (ce *condAddrEncoder) encode(e *encodeState, v reflect.Value) {
	if v.CanAddr() {
		ce.canAddrEnc(e, v)
	} else {
		ce.elseEnc(e, v)
	}
}

func newCondAddrEncoder(canAddrEnc, elseEnc encoderFunc) encoderFunc {
	enc := &condAddrEncoder{canAddrEnc: canAddrEnc, elseEnc: elseEnc}
	return enc.encode
}

func marshalEncoder(e *encodeState, v reflect.Value) {
	if v.Kind() == reflect.Ptr && v.IsNil() {
		e.WriteByte(Nil)
		return
	}
	m := v.Interface().(Marshaler)
	b, err := m.MarshalBinaryPack()
	if err == nil {
		_, err = e.Write(b)
	}
	if err != nil {
		e.error(&MarshalerError{v.Type(), err})
	}
}

func addrMarshalerEncoder(e *encodeState, v reflect.Value) {
	va := v.Addr()
	if va.IsNil() {
		nilEncoder(e, v)
		return
	}
	m := va.Interface().(Marshaler)
	b, err := m.MarshalBinaryPack()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err})
	}
	e.Write(b)
}

func textMarshalerEncoder(e *encodeState, v reflect.Value) {
	if v.Kind() == reflect.Ptr && v.IsNil() {
		e.WriteByte(Nil)
		return
	}
	m := v.Interface().(encoding.TextMarshaler)
	b, err := m.MarshalText()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err})
	}
	e.stringBytes(b)
}

func addrTextMarshalerEncoder(e *encodeState, v reflect.Value) {
	va := v.Addr()
	if va.IsNil() {
		nilEncoder(e, v)
		return
	}
	m := va.Interface().(encoding.TextMarshaler)
	b, err := m.MarshalText()
	if err != nil {
		e.error(&MarshalerError{v.Type(), err})
	}
	e.stringBytes(b)
}

func invalidValueEncoder(e *encodeState, v reflect.Value) {
	e.WriteByte(Nil)
}

func nilEncoder(e *encodeState, v reflect.Value) {
	e.WriteByte(Nil)
}

func boolEncoder(e *encodeState, v reflect.Value) {
	if v.Bool() {
		e.WriteByte(True)
	} else {
		e.WriteByte(False)
	}
}

func floatEncoder(e *encodeState, v reflect.Value) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 32)})
	}
	e.WriteByte(Float32)
	e.writeUint32(math.Float32bits(float32(f)))
}

func doubleEncoder(e *encodeState, v reflect.Value) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		e.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 64)})
	}
	e.WriteByte(Float64)
	e.writeUint64(math.Float64bits(f))
}

func uint8Encoder(e *encodeState, v reflect.Value) {
	e.Write([]byte{0xcc, uint8(v.Uint())})
}

func uint16Encoder(e *encodeState, v reflect.Value) {
	e.WriteByte(Uint16)
	e.writeUint16(uint16(v.Uint()))
}

func uint32Encoder(e *encodeState, v reflect.Value) {
	e.WriteByte(Uint32)
	e.writeUint32(uint32(v.Uint()))
}

func uint64Encoder(e *encodeState, v reflect.Value) {
	e.WriteByte(Uint64)
	e.writeUint64(v.Uint())
}

func intEncoder(e *encodeState, v reflect.Value) {
	i := v.Int()
	switch {
	case i < math.MinInt32 || i > math.MaxInt32:
		e.WriteByte(0xd3)
		binary.BigEndian.PutUint64(e.s8, uint64(i))
		e.Write(e.s8)
	case i < math.MinInt16 || i > math.MaxInt16:
		e.WriteByte(0xd2)
		binary.BigEndian.PutUint32(e.s4, uint32(i))
		e.Write(e.s4)
	case i < math.MinInt8 || i > math.MaxInt8:
		e.WriteByte(0xd1)
		binary.BigEndian.PutUint16(e.s2, uint16(i))
		e.Write(e.s2)
	case i < -32:
		e.Write([]byte{0xd0, byte(i)})
	case i >= -32 && i <= math.MaxInt8:
		e.WriteByte(byte(i))
	default:
		e.error(errors.New("binarypack: unreachable in intEncoder"))
	}
}

func uintEncoder(e *encodeState, v reflect.Value) {
	u := v.Uint()
	switch {
	case u <= math.MaxInt8:
		e.WriteByte(byte(u))
	case u <= math.MaxUint8:
		e.Write([]byte{0xcc, byte(u)})
	case u <= math.MaxUint16:
		e.WriteByte(0xcd)
		binary.BigEndian.PutUint16(e.s2, uint16(u))
		e.Write(e.s2)
	case u <= math.MaxUint32:
		e.WriteByte(0xce)
		binary.BigEndian.PutUint32(e.s4, uint32(u))
		e.Write(e.s4)
	default:
		e.WriteByte(0xcf)
		binary.BigEndian.PutUint64(e.s8, uint64(u))
		e.Write(e.s8)
	}
}

func stringEncoder(e *encodeState, v reflect.Value) {
	e.stringBytes([]byte(v.String()))
}

func mapEncoder(e *encodeState, v reflect.Value) {
	if v.IsNil() {
		nilEncoder(e, v)
		return
	}
	l := v.Len()
	switch {
	case l < 16:
		e.WriteByte(0x80 | byte(l))
	case l < math.MaxUint16:
		e.WriteByte(0xde)
		binary.BigEndian.PutUint16(e.s2, uint16(l))
		e.Write(e.s2)
	default:
		e.WriteByte(0xdf)
		binary.BigEndian.PutUint32(e.s4, uint32(l))
		e.Write(e.s4)
	}
	for _, k := range v.MapKeys() {
		w := v.MapIndex(k)
		typeEncoder(k.Type())(e, k)
		typeEncoder(w.Type())(e, w)
	}
}

type ptrEncoder struct {
	elemEnc encoderFunc
}

func (pe *ptrEncoder) encode(e *encodeState, v reflect.Value) {
	if v.IsNil() {
		nilEncoder(e, v)
		return
	}
	pe.elemEnc(e, v.Elem())
}

func newPtrEncoder(t reflect.Type) encoderFunc {
	enc := &ptrEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

var byteSliceType = reflect.TypeOf([]byte{})

func sliceEncoder(e *encodeState, v reflect.Value) {
	if v.IsNil() {
		nilEncoder(e, v)
		return
	}
	if v.Type() == byteSliceType {
		e.rawBytes(v.Bytes())
		return
	}
	l := v.Len()
	switch {
	case l < 16:
		e.WriteByte(0x90 | byte(l))
	case l < math.MaxUint16:
		e.WriteByte(0xdc)
		binary.BigEndian.PutUint16(e.s2, uint16(l))
		e.Write(e.s2)
	default:
		e.WriteByte(0xdd)
		binary.BigEndian.PutUint32(e.s4, uint32(l))
		e.Write(e.s4)
	}
	for i := 0; i < l; i++ {
		f := v.Index(i)
		typeEncoder(f.Type())(e, f)
	}
}

var (
	timeStructType = reflect.TypeOf(time.Time{})
	stringType     = reflect.TypeOf("")
)

type structEncoder struct {
	fields        []field
	fieldEncoding []encoderFunc
}

func (se *structEncoder) encode(e *encodeState, v reflect.Value) {
	/*
		if v.IsNil() {
			nilEncoder(e, v)
			return
		}
	*/
	if v.Type() == timeStructType {
		tm := v.Interface().(time.Time)
		ts := tm.Format("Mon Jan 02 2006 15:04:05") + " GMT" + tm.Format("-0700 (MST)")
		tv := reflect.ValueOf(ts)
		valueEncoder(tv)(e, tv)
		return
	}

	keys := make([]string, 0)
	vals := make([]reflect.Value, 0)
	size := 0
	for _, f := range se.fields {
		fv := v.FieldByIndex(f.index)
		if f.omitEmpty && isEmptyValue(fv) {
			continue
		}
		keys = append(keys, f.name)
		vals = append(vals, fv)
		size++
	}

	switch {
	case size < 16:
		e.WriteByte(0x80 | byte(size))
	case size < math.MaxUint16:
		e.WriteByte(0xde)
		binary.BigEndian.PutUint16(e.s2, uint16(size))
		e.Write(e.s2)
	default:
		e.WriteByte(0xdf)
		binary.BigEndian.PutUint32(e.s4, uint32(size))
		e.Write(e.s4)
	}
	for i := 0; i < size; i++ {
		kv := reflect.ValueOf(keys[i])
		valueEncoder(kv)(e, kv)
		valueEncoder(vals[i])(e, vals[i])
	}
}

func newStructEncoder(t reflect.Type) encoderFunc {
	fields := typeFields(t)
	se := &structEncoder{
		fields:        fields,
		fieldEncoding: make([]encoderFunc, len(fields)),
	}
	for i, f := range fields {
		se.fieldEncoding[i] = typeEncoder(typeByIndex(t, f.index))
	}
	return se.encode
}

// A field represents a single field found in a struct.
type field struct {
	name      string
	nameBytes []byte // []byte(name)
	tag       bool
	index     []int
	typ       reflect.Type
	omitEmpty bool
}

func fillField(f field) field {
	f.nameBytes = []byte(f.name)
	return f
}

// byName sorts field by name.
type byName []field

func (x byName) Len() int { return len(x) }

func (x byName) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

func (x byName) Less(i, j int) bool {
	if x[i].name != x[j].name {
		return x[i].name < x[j].name
	}
	if len(x[i].index) != len(x[j].index) {
		return len(x[i].index) < len(x[j].index)
	}
	if x[i].tag != x[j].tag {
		return x[i].tag
	}
	return byIndex(x).Less(i, j)
}

// byIndex sorts field by index sequence.
type byIndex []field

func (x byIndex) Len() int { return len(x) }

func (x byIndex) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

func (x byIndex) Less(i, j int) bool {
	for k, xik := range x[i].index {
		if k >= len(x[j].index) {
			return false
		}
		if xik != x[j].index[k] {
			return xik < x[j].index[k]
		}
	}
	return len(x[i].index) < len(x[j].index)
}

func typeFields(t reflect.Type) []field {
	curr := []field{}
	next := []field{{typ: t}}
	size := map[reflect.Type]int{}
	sizeNext := map[reflect.Type]int{}
	seen := map[reflect.Type]bool{}

	var fields []field
	for len(next) > 0 {
		curr, next = next, curr[:0]
		size, sizeNext = sizeNext, map[reflect.Type]int{}

		for _, f := range curr {
			if seen[f.typ] {
				continue
			}
			seen[f.typ] = true

			// Scan f.typ for fields to include
			for i := 0; i < f.typ.NumField(); i++ {
				sf := f.typ.Field(i)
				if sf.PkgPath != "" && sf.Anonymous { // unexported
					continue
				}
				tag := sf.Tag.Get("binarypack")
				if tag == "-" {
					continue
				}
				name, opts := parseTag(tag)
				index := make([]int, len(f.index)+1)
				copy(index, f.index)
				index[len(f.index)] = i

				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Ptr {
					// Follow pointer.
					ft = ft.Elem()
				}

				// Record found field and index sequence.
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					tagged := name != ""
					if name == "" {
						name = sf.Name
					}
					fields = append(fields, fillField(field{
						name:      name,
						tag:       tagged,
						index:     index,
						typ:       ft,
						omitEmpty: opts.Contains("omitempty"),
					}))
					if size[f.typ] > 1 {
						fields = append(fields, fields[len(fields)-1])
					}
					continue
				}

				sizeNext[ft]++
				if sizeNext[ft] == 1 {
					next = append(next, fillField(field{name: ft.Name(), index: index, typ: ft}))
				}
			}
		}
	}

	sort.Sort(byName(fields))

	// Delete all fields that are hidden by the Go rules for embedded
	// fields, except that fields with pack tags are promoted.
	out := fields[:0]
	for advance, i := 0, 0; i < len(fields); i += advance {
		fi := fields[i]
		name := fi.name
		for advance = 1; i+advance < len(fields); advance++ {
			fj := fields[i+advance]
			if fj.name != name {
				break
			}
		}
		if advance == 1 { // Only one field with this name
			out = append(out, fi)
			continue
		}
		dominant, ok := dominantField(fields[i : i+advance])
		if ok {
			out = append(out, dominant)
		}
	}

	fields = out
	sort.Sort(byIndex(fields))

	return fields
}

// dominantField looks through the fields, all of which are known to have the
// same name, to find the single field that dominates the others using Go's
// embedding rules, modified by the presence of pack tags.
func dominantField(fields []field) (field, bool) {
	// The fields are sorted in increasing index-length order.
	length := len(fields[0].index)
	tagged := -1 // Index of first tagged field.
	for i, f := range fields {
		if len(f.index) > length {
			fields = fields[:i]
			break
		}
		if f.tag {
			if tagged >= 0 {
				// Multiple tagged fields at the same level:
				// conflict.
				return field{}, false
			}
			tagged = i
		}
	}
	if tagged >= 0 {
		return fields[tagged], true
	}
	// All remaining fields have the same length. If there's more than one,
	// we have a conflict.
	if len(fields) > 1 {
		return field{}, false
	}
	return fields[0], true
}

func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

func typeByIndex(t reflect.Type, index []int) reflect.Type {
	for _, i := range index {
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		t = t.Field(i).Type
	}
	return t
}

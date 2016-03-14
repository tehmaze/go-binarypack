package binarypack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
)

func Unmarshal(b []byte, v ...interface{}) error {
	d := new(decodeState)
	d.Write(b)

	for _, vv := range v {
		if err := d.unmarshal(vv); err != nil {
			return err
		}
	}
	return nil
}

func errDecodeOp(op byte) error {
	return fmt.Errorf("binarypack: illegal opcode %#04x", op)
}

func errDecodeValue(v reflect.Value, t string) error {
	if v.Kind() == reflect.Ptr {
		return fmt.Errorf("binarypack: don't know how to decode %s to *%v", t, v.Elem().Kind())
	}
	return fmt.Errorf("binarypack: don't know how to decode %s to %v", t, v.Kind())
}

type decodeState struct {
	bytes.Buffer
	offset     int
	savedError error
}

func (d *decodeState) unmarshal(i interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(r)
			}
			err = r.(error)
		}
	}()

	v, ok := i.(reflect.Value)
	if !ok {
		v = reflect.ValueOf(i)
	}
	if !v.IsValid() {
		return fmt.Errorf("binarypack: ummarshal to invalid %v", i)
	}
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("binarypack: expected pointer, got %T", i)
	}
	d.decodeValue(v)
	return err
}

func (d *decodeState) decodeValue(v reflect.Value) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	op, err := d.ReadByte()
	if err != nil {
		panic(err)
	}

	switch {
	case op < 0x80:
		d.decodeUint(v, uint64(op))
		return
	case op <= FixMap+15:
		d.decodeMap(v, uint32(op-FixMap))
		return
	case op <= FixArray+15:
		d.decodeArray(v, uint32(op-FixArray))
		return
	case op <= FixRaw+15:
		d.decodeRaw(v, uint32(op-FixRaw))
		return
	case op <= FixString+15:
		d.decodeRaw(v, uint32(op-FixString))
		return
	case op >= FixNeg:
		d.decodeInt(v, int64(int8(op)))
		return
	}

	switch op {
	case Nil, Undefined:
		d.decodeNil(v)
	case False:
		d.decodeBool(v, false)
	case True:
		d.decodeBool(v, true)
	case Int8:
		size, err := d.ReadByte()
		if err != nil {
			panic(err)
		}
		d.decodeInt(v, int64(size))
	case Int16:
		size, err := d.readUint16()
		if err != nil {
			panic(err)
		}
		d.decodeInt(v, int64(size))
	case Int32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeInt(v, int64(size))
	case Int64:
		size, err := d.readUint64()
		if err != nil {
			panic(err)
		}
		d.decodeInt(v, int64(size))
	case Uint8:
		size, err := d.ReadByte()
		if err != nil {
			panic(err)
		}
		d.decodeUint(v, uint64(size))
	case Uint16:
		size, err := d.readUint16()
		if err != nil {
			panic(err)
		}
		d.decodeUint(v, uint64(size))
	case Uint32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeUint(v, uint64(size))
	case Uint64:
		size, err := d.readUint64()
		if err != nil {
			panic(err)
		}
		d.decodeUint(v, uint64(size))
	case String16:
		size, err := d.readUint16()
		if err != nil {
			panic(err)
		}
		d.decodeRaw(v, uint32(size))
	case String32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeRaw(v, size)
	case Raw16:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeRaw(v, uint32(size))
	case Raw32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeRaw(v, size)
	case Array16:
		size, err := d.readUint16()
		if err != nil {
			panic(err)
		}
		d.decodeArray(v, uint32(size))
	case Array32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeArray(v, size)
	case Map16:
		size, err := d.readUint16()
		if err != nil {
			panic(err)
		}
		d.decodeMap(v, uint32(size))
	case Map32:
		size, err := d.readUint32()
		if err != nil {
			panic(err)
		}
		d.decodeMap(v, size)
	default:
		panic(errDecodeOp(op))
	}
}

func (d *decodeState) decodeArray(v reflect.Value, size uint32) {
	var e reflect.Value

	switch v.Kind() {
	case reflect.Slice:
		e = reflect.MakeSlice(v.Type(), int(size), int(size))
	case reflect.Interface:
		e = reflect.ValueOf(make([]interface{}, int(size)))
	default:
		panic(errDecodeValue(v, "array"))
	}

	for i := 0; i < int(size); i++ {
		d.unmarshal(e.Index(i).Addr().Interface())
	}
	v.Set(e)
}

func (d *decodeState) decodeBool(v reflect.Value, b bool) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(b)
	case reflect.String:
		v.SetString(strconv.FormatBool(b))
	case reflect.Interface:
		v.Set(reflect.ValueOf(b))
	default:
		panic(errDecodeValue(v, "bool"))
	}
}

func (d *decodeState) decodeMap(v reflect.Value, size uint32) {
	var e reflect.Value

	switch v.Kind() {
	case reflect.Map:
		e = reflect.MakeMap(v.Type())
	case reflect.Struct:
		d.decodeStruct(v, size)
		return
	case reflect.Interface:
		e = reflect.ValueOf(make(map[interface{}]interface{}))
	default:
		panic(errDecodeValue(v, "map"))
	}

	for i := 0; i < int(size); i++ {
		kt := reflect.New(e.Type().Key())
		vt := reflect.New(e.Type().Elem())
		if err := d.unmarshal(kt.Interface()); err != nil {
			panic(err)
		}
		if err := d.unmarshal(vt.Interface()); err != nil {
			panic(err)
		}
		e.SetMapIndex(kt.Elem(), vt.Elem())
	}
	v.Set(e)
}

func (d *decodeState) decodeNil(v reflect.Value) {
	v.Set(reflect.Zero(v.Type()))
}

func (d *decodeState) decodeRaw(v reflect.Value, size uint32) {
	var r = make([]byte, size)
	if _, err := d.Read(r); err != nil {
		panic(err)
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(string(r))
	case reflect.Slice:
		v.SetBytes(r)
	case reflect.Interface:
		v.Set(reflect.ValueOf(string(r)))
	default:
		panic(errDecodeValue(v, "raw"))
	}
}

func (d *decodeState) decodeStruct(v reflect.Value, size uint32) {
	t := v.Type()
	fl := t.NumField()
	fs := make(map[string]reflect.Value)

	for i := 0; i < fl; i++ {
		f := t.Field(i)
		k := f.Tag.Get("binarypack")

		if f.PkgPath != "" || f.Anonymous {
			continue
		}

		switch k {
		case "-":
			continue
		case "":
			k = f.Name
		}
		fs[k] = v.Field(i)
	}

	for i := 0; i < int(size); i++ {
		var k string
		if err := d.unmarshal(&k); err != nil {
			panic(err)
		}

		vt := reflect.New(fs[k].Type())
		if err := d.unmarshal(vt.Interface()); err != nil {
			panic(err)
		}

		if f, ok := fs[k]; ok {
			f.Set(vt.Elem())
		}
	}
}

func (d *decodeState) decodeInt(v reflect.Value, i int64) {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(i))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(i)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(i))
	case reflect.String:
		v.SetString(strconv.FormatInt(i, 10))
	case reflect.Interface:
		v.Set(reflect.ValueOf(i))
	default:
		panic(errDecodeValue(v, "int"))
	}
}

func (d *decodeState) decodeUint(v reflect.Value, u uint64) {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(u)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(u))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(u))
	case reflect.String:
		v.SetString(strconv.FormatUint(u, 10))
	case reflect.Interface:
		v.Set(reflect.ValueOf(u))
	default:
		panic(errDecodeValue(v, "uint"))
	}
}

func (d *decodeState) readUint16() (uint16, error) {
	var b = make([]byte, 2)
	if _, err := d.Read(b); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (d *decodeState) readUint32() (uint32, error) {
	var b = make([]byte, 4)
	if _, err := d.Read(b); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (d *decodeState) readUint64() (uint64, error) {
	var b = make([]byte, 8)
	if _, err := d.Read(b); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

func reflectValue(v interface{}) (rv reflect.Value) {
	var ok bool

	if rv, ok = v.(reflect.Value); !ok {
		rv = reflect.ValueOf(v)
	}
	return
}

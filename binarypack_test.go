package binarypack

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

type teststruct struct {
	Int    int    `binarypack:"int"`
	Int8   int    `binarypack:"fixint"`
	Int16  int16  `binarypack:"int6"`
	Int32  int32  `binarypack:"int32"`
	Int64  int64  `binarypack:"int64"`
	Uint   uint   `binarypack:"uint"`
	Uint16 uint16 `binarypack:"uint16"`
	Uint32 uint32 `binarypack:"uint32"`
	Uint64 uint64 `binarypack:"uint64"`
}

var tests = []struct {
	name string
	test interface{}
	want []byte
}{
	//{"Nil", nil, []byte{Nil}},
	{"False", false, []byte{False}},
	{"True", true, []byte{True}},
	{"FixInt", 42, []byte{42}},
	//{"FixNeg", -23, []byte{0xff}},
	{"FixRaw", []byte("gopher"), []byte("\xa6gopher")},
	{"FixString", "gopher", []byte("\xb6gopher")},
}

func TestMarshal(t *testing.T) {
	for _, test := range tests {
		got, err := Marshal(test.test)
		if err != nil {
			t.Fatalf("%s: %v\n", test.name, err)
		}
		if bytes.Equal(got, test.want) {
			t.Logf("%s: ok\n", test.name)
		} else {
			t.Fatalf("%s: expected %v, got %v", test.name, test.want, got)
		}
	}
}

func TestUnmarshal(t *testing.T) {

	for _, test := range tests {
		var got = reflect.New(reflect.TypeOf(test.test))
		if err := Unmarshal(test.want, got); err != nil {
			t.Fatalf("%s: %v\n", test.name, err)
		} else {
			t.Logf("%s: ok\n", test.name)
		}
	}
}

func TestStruct(t *testing.T) {
	var s = teststruct{0x7f,
		math.MaxInt8, math.MaxInt16, math.MaxInt32, math.MaxInt64,
		math.MaxUint8, math.MaxUint16, math.MaxUint32, math.MaxUint64,
	}

	b, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty buffer")
	}

	t.Logf("bytes: %v\n", b)

	d := teststruct{}
	if err := Unmarshal(b, &d); err != nil {
		t.Fatalf("unmarshal: %v\n", err)
	}
	if !reflect.DeepEqual(s, d) {
		t.Fatalf("not equal %v and %v\n", s, d)
	}
}

package binarypack

const (
	FixMap      = 0x80
	FixArray    = 0x90
	FixRaw      = 0xa0
	FixString   = 0xb0
	Nil         = 0xc0
	Undefined   = 0xc1
	False       = 0xc2
	True        = 0xc3
	Float32     = 0xca
	Float64     = 0xcb
	Uint8       = 0xcc
	Uint16      = 0xcd
	Uint32      = 0xce
	Uint64      = 0xcf
	Int8        = 0xd0
	Int16       = 0xd1
	Int32       = 0xd2
	Int64       = 0xd3
	UndefinedD4 = 0xd4
	UndefinedD5 = 0xd5
	UndefinedD6 = 0xd6
	UndefinedD7 = 0xd7
	String16    = 0xd8
	String32    = 0xd9
	Raw16       = 0xda
	Raw32       = 0xdb
	Array16     = 0xdc
	Array32     = 0xdd
	Map16       = 0xde
	Map32       = 0xdf
	FixNeg      = 0xe0
)

// Package binarypack is almost msgpack, suitable for talking to binary.js
package binarypack

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"
)

type BinaryPack struct {
	cursor cursor
}

func New(b []byte) *BinaryPack {
	if b == nil {
		b = make([]byte, 0)
	}
	return &BinaryPack{cursor{buffer: b}}
}

// EOF returns true if the whole buffer is consumed.
func (b *BinaryPack) EOF() bool {
	return b.cursor.eof()
}

// Bytes returns the contents of the binarypack buffer.
func (b *BinaryPack) Bytes() []byte {
	return b.cursor.buffer
}

// Marshal writes a message to the binarypack buffer.
func (b *BinaryPack) Marshal(v interface{}) error {
	switch t := v.(type) {
	case int:
		switch {
		case t < 0x80:
			b.cursor.writeByte(byte(t))
			return nil
		case t >= math.MinInt16 && t <= math.MaxInt16:
			return b.Marshal(int16(t))
		case t >= math.MinInt32 && t <= math.MaxInt32:
			return b.Marshal(int32(t))
		default:
			return b.Marshal(int64(t))
		}
	case nil:
		b.cursor.writeByte(0xc0)
		return nil
	case bool:
		if t {
			b.cursor.writeByte(0xc3)
		} else {
			b.cursor.writeByte(0xc2)
		}
		return nil
	case float32:
		b.cursor.writeByte(0xca)
		b.writeUint32(math.Float32bits(t))
		return nil
	case float64:
		b.cursor.writeByte(0xcb)
		b.writeUint64(math.Float64bits(t))
		return nil
	case uint8:
		b.cursor.write([]byte{0xcc, t})
		return nil
	case uint16:
		b.cursor.writeByte(0xcd)
		b.writeUint16(t)
		return nil
	case uint32:
		b.cursor.writeByte(0xce)
		b.writeUint32(t)
		return nil
	case uint64:
		b.cursor.writeByte(0xcf)
		b.writeUint64(t)
		return nil
	case int8:
		b.cursor.write([]byte{0xd0, uint8(t)})
		return nil
	case int16:
		b.cursor.writeByte(0xd1)
		b.writeUint16(uint16(t))
		return nil
	case int32:
		b.cursor.writeByte(0xd2)
		b.writeUint32(uint32(t))
		return nil
	case int64:
		b.cursor.writeByte(0xd3)
		b.writeUint64(uint64(t))
		return nil
	case string:
		var size = len(t)
		switch {
		case size <= 0x0f:
			b.cursor.writeByte(0xb0 | byte(size))
			b.cursor.write([]byte(t))
		case size <= math.MaxUint16:
			b.cursor.writeByte(0xd8)
			b.writeUint16(uint16(size))
			b.cursor.write([]byte(t))
		default:
			b.cursor.writeByte(0xd9)
			b.writeUint32(uint32(size))
			b.cursor.write([]byte(t))
		}
		return nil
	case []byte:
		var size = len(t)
		switch {
		case size <= 0x0f:
			b.cursor.writeByte(0xa0 | byte(size))
			b.cursor.write(t)
		case size <= math.MaxUint16:
			b.cursor.writeByte(0xda)
			b.writeUint16(uint16(size))
			b.cursor.write(t)
		default:
			b.cursor.writeByte(0xdb)
			b.writeUint32(uint32(size))
			b.cursor.write(t)
		}
		return nil
	default:
		switch reflect.TypeOf(t).Kind() {
		case reflect.Slice:
			s := reflect.ValueOf(t)
			l := s.Len()
			switch {
			case l <= 0x0f:
				b.cursor.writeByte(0x90 | byte(l))
			case l <= math.MaxUint16:
				b.cursor.writeByte(0xdc)
				b.writeUint16(uint16(l))
			default:
				b.cursor.writeByte(0xdd)
				b.writeUint32(uint32(l))
			}
			for i := 0; i < l; i++ {
				if err := b.Marshal(s.Index(i).Interface()); err != nil {
					return err
				}
			}
			return nil

		case reflect.Map:
			v := reflect.ValueOf(t)
			k := v.MapKeys()
			l := len(k)
			switch {
			case l <= 0x0f:
				b.cursor.writeByte(0x80 | byte(l))
			case l <= math.MaxUint16:
				b.cursor.writeByte(0xde)
				b.Marshal(uint16(l))
			default:
				b.cursor.writeByte(0xdf)
				b.Marshal(uint32(l))
			}
			for i := 0; i < l; i++ {
				if err := b.Marshal(k[i].Interface()); err != nil {
					return err
				}
				if err := b.Marshal(v.MapIndex(k[i]).Interface()); err != nil {
					return err
				}
			}
			return nil

		}
	}

	return fmt.Errorf("binarypack: can't marshal %T", v)
}

// UnmarshalNext reads the next binarypack message from the buffer.
func (b *BinaryPack) UnmarshalNext() (interface{}, error) {
	t, err := b.cursor.next()
	if err != nil {
		return nil, err
	}

	switch {
	case t < 0x80:
		return int(t), nil
	case (t ^ 0xe0) < 0x20:
		return int((t ^ 0xe0) - 0x20), nil
	}

	if size := t ^ 0xa0; size <= 0x0f {
		return b.readBytes(int(size))
	} else if size := t ^ 0xb0; size <= 0x0f {
		return b.readString(int(size))
	} else if size := t ^ 0x90; size <= 0x0f {
		return b.readArray(int(size))
	} else if size := t ^ 0x80; size <= 0x0f {
		return b.readMap(int(size))
	}

	switch t {
	case 0xc0, 0xc1: // There is no undefined in Go
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xca:
		return b.readFloat()
	case 0xcb:
		return b.readDouble()
	case 0xcc:
		return b.readUint8()
	case 0xcd:
		return b.readUint16()
	case 0xce:
		return b.readUint32()
	case 0xcf:
		return b.readUint64()
	case 0xd0:
		return b.readInt8()
	case 0xd1:
		return b.readInt16()
	case 0xd2:
		return b.readInt32()
	case 0xd3:
		return b.readInt64()
	case 0xd4, 0xd5, 0xd6, 0xd7:
		return nil, nil
	case 0xd8:
		n, err := b.readUint16()
		if err != nil {
			return nil, err
		}
		return b.readString(int(n))
	case 0xd9:
		n, err := b.readUint32()
		if err != nil {
			return nil, err
		}
		return b.readString(int(n))
	case 0xda:
		n, err := b.readUint16()
		if err != nil {
			return nil, err
		}
		return b.readBytes(int(n))
	case 0xdb:
		n, err := b.readUint32()
		if err != nil {
			return nil, err
		}
		return b.readBytes(int(n))
	case 0xdc:
		n, err := b.readUint16()
		if err != nil {
			return nil, err
		}
		return b.readArray(int(n))
	case 0xdd:
		n, err := b.readUint32()
		if err != nil {
			return nil, err
		}
		return b.readArray(int(n))
	case 0xde:
		n, err := b.readUint16()
		if err != nil {
			return nil, err
		}
		return b.readMap(int(n))
	case 0xdf:
		n, err := b.readUint32()
		if err != nil {
			return nil, err
		}
		return b.readMap(int(n))
	}

	return nil, fmt.Errorf("binarypack: %#04x unsupported", t)
}

func (b *BinaryPack) readInt8() (int8, error) {
	i, err := b.cursor.next()
	return int8(i), err
}

func (b *BinaryPack) readInt16() (int16, error) {
	i, err := b.readUint16()
	return int16(i), err
}

func (b *BinaryPack) readInt32() (int32, error) {
	i, err := b.readUint32()
	return int32(i), err
}

func (b *BinaryPack) readInt64() (int64, error) {
	i, err := b.readUint64()
	return int64(i), err
}

func (b *BinaryPack) readUint8() (uint8, error) {
	i, err := b.cursor.next()
	return uint8(i), err
}

func (b *BinaryPack) readUint16() (uint16, error) {
	i, err := b.cursor.slice(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(i), err
}

func (b *BinaryPack) readUint32() (uint32, error) {
	i, err := b.cursor.slice(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(i), err
}

func (b *BinaryPack) readUint64() (uint64, error) {
	i, err := b.cursor.slice(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(i), err
}

func (b *BinaryPack) readFloat() (float32, error) {
	i, err := b.readUint32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(i), nil
}

func (b *BinaryPack) readDouble() (float64, error) {
	i, err := b.readUint64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(i), nil
}

func (b *BinaryPack) readBytes(n int) ([]byte, error) {
	return b.cursor.slice(n)
}

func (b *BinaryPack) readString(n int) (string, error) {
	s, err := b.cursor.slice(n)
	if err != nil {
		return "", err
	}
	return string(s), nil
}

func (b *BinaryPack) readArray(n int) ([]interface{}, error) {
	var a = make([]interface{}, n)
	for i := 0; i < n; i++ {
		v, err := b.UnmarshalNext()
		if err != nil {
			return nil, err
		}
		a[i] = v
	}
	return a, nil
}

func (b *BinaryPack) readMap(n int) (map[interface{}]interface{}, error) {
	var m = make(map[interface{}]interface{})
	for i := 0; i < n; i++ {
		k, err := b.UnmarshalNext()
		if err != nil {
			return nil, err
		}
		v, err := b.UnmarshalNext()
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

func (b *BinaryPack) writeUint16(u uint16) {
	b.cursor.write([]byte{byte(u >> 8), byte(u)})
}

func (b *BinaryPack) writeUint32(u uint32) {
	b.cursor.write([]byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)})
}

func (b *BinaryPack) writeUint64(u uint64) {
	b.writeUint32(uint32(u >> 32))
	b.writeUint32(uint32(u))
}

type cursor struct {
	buffer []byte
	cursor int
}

func (c *cursor) eof() bool {
	return c.cursor == len(c.buffer)
}

func (c *cursor) next() (byte, error) {
	if c.cursor >= len(c.buffer) {
		return 0, io.EOF
	}
	var b = c.buffer[c.cursor]
	c.cursor++
	return b, nil
}

func (c *cursor) slice(n int) ([]byte, error) {
	if c.cursor+n > len(c.buffer) {
		return nil, io.EOF
	}
	var b = c.buffer[c.cursor : c.cursor+n]
	c.cursor += n
	return b, nil
}

func (c *cursor) write(b []byte) {
	c.buffer = append(c.buffer, b...)
}

func (c *cursor) writeByte(b byte) {
	c.buffer = append(c.buffer, b)
}

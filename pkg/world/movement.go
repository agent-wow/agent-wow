package world

import (
	"encoding/binary"
	"errors"
	"math"
)

// Only control acknowledgments use MovementInfo in this version. Optional
// fields are decoded completely so a teleport cannot desynchronize parsing.
type movement struct {
	flags                         uint32
	extra                         uint16
	pos                           [4]float32
	transportGUID                 uint64
	transportPos                  [4]float32
	transportTime, transportTime2 uint32
	seat                          uint8
	pitch                         float32
	fallTime                      uint32
	jump                          [4]float32
	elevation                     float32
}

type decoder struct {
	data []byte
	err  error
}

func (r *decoder) take(n int) []byte {
	if r.err != nil || n > len(r.data) {
		r.err = errors.New("truncated packet")
		return make([]byte, n)
	}
	v := r.data[:n]
	r.data = r.data[n:]
	return v
}
func (r *decoder) u8() uint8   { return r.take(1)[0] }
func (r *decoder) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *decoder) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *decoder) f32() float32 {
	v := math.Float32frombits(r.u32())
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		r.err = errors.New("non-finite coordinate")
	}
	return v
}
func (r *decoder) position() [4]float32 { return [4]float32{r.f32(), r.f32(), r.f32(), r.f32()} }
func (r *decoder) guid() uint64 {
	mask := r.u8()
	var guid uint64
	for i := range 8 {
		if mask&(1<<i) != 0 {
			guid |= uint64(r.u8()) << (i * 8)
		}
	}
	return guid
}
func (r *decoder) finish() error {
	if r.err != nil {
		return r.err
	}
	if len(r.data) != 0 {
		return errors.New("trailing packet data")
	}
	return nil
}
func (r *decoder) movement() movement {
	m := movement{flags: r.u32(), extra: r.u16()}
	r.u32() // server timestamp; client replies use its own monotonic clock
	m.pos = r.position()
	if m.flags&0x200 != 0 {
		m.transportGUID, m.transportPos = r.guid(), r.position()
		m.transportTime, m.seat = r.u32(), r.u8()
		if m.extra&0x400 != 0 {
			m.transportTime2 = r.u32()
		}
	}
	if m.flags&0x02200000 != 0 || m.extra&0x20 != 0 {
		m.pitch = r.f32()
	}
	m.fallTime = r.u32()
	if m.flags&0x1000 != 0 {
		m.jump = r.position()
	}
	if m.flags&0x04000000 != 0 {
		m.elevation = r.f32()
	}
	return m
}
func appendGUID(dst []byte, guid uint64) []byte {
	start := len(dst)
	dst = append(dst, 0)
	for i := range 8 {
		if b := byte(guid >> (i * 8)); b != 0 {
			dst[start] |= 1 << i
			dst = append(dst, b)
		}
	}
	return dst
}
func appendFloat(dst []byte, f float32) []byte {
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(f))
}
func appendPosition(dst []byte, pos [4]float32) []byte {
	for _, f := range pos {
		dst = appendFloat(dst, f)
	}
	return dst
}
func (m movement) appendTo(dst []byte, timestamp uint32) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, m.flags)
	dst = binary.LittleEndian.AppendUint16(dst, m.extra)
	dst = binary.LittleEndian.AppendUint32(dst, timestamp)
	dst = appendPosition(dst, m.pos)
	if m.flags&0x200 != 0 {
		dst = appendGUID(dst, m.transportGUID)
		dst = appendPosition(dst, m.transportPos)
		dst = binary.LittleEndian.AppendUint32(dst, m.transportTime)
		dst = append(dst, m.seat)
		if m.extra&0x400 != 0 {
			dst = binary.LittleEndian.AppendUint32(dst, m.transportTime2)
		}
	}
	if m.flags&0x02200000 != 0 || m.extra&0x20 != 0 {
		dst = appendFloat(dst, m.pitch)
	}
	dst = binary.LittleEndian.AppendUint32(dst, m.fallTime)
	if m.flags&0x1000 != 0 {
		dst = appendPosition(dst, m.jump)
	}
	if m.flags&0x04000000 != 0 {
		dst = appendFloat(dst, m.elevation)
	}
	return dst
}

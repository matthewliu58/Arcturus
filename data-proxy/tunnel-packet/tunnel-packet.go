package tunnel_packet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// b[0]: HopPos(1) + b[1:17]: HopIP(16) + b[17:19]: PayloadLen(2) + b[19:21]: Port(2) + b[21:24]: padding(3)
const (
	HeaderSize = 24
	MaxHops    = 4
)

var ErrInvalidHeader = errors.New("invalid packet header: length too short")

type Packet struct {
	Buf        []byte
	BuffSize   int
	HopPos     byte
	HopIP      [4]uint32
	PayloadLen uint16
	Port       uint16
	wp         int
}

type SubPacket struct {
	UserID uint32
	Data   []byte
}

func NewPacket(buffSizes int) *Packet {
	return &Packet{
		BuffSize: buffSizes,
		Buf:      make([]byte, buffSizes),
		wp:       HeaderSize,
	}
}

func (p *Packet) SetHopIP(hopIdx int, ip net.IP) {
	if hopIdx < 0 || hopIdx >= MaxHops {
		return
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
	p.HopIP[hopIdx] = binary.BigEndian.Uint32(ip4)
}

func (p *Packet) SetHopPos(pos byte) {
	if pos >= 0 && pos < MaxHops {
		p.HopPos = pos
	}
}

func (p *Packet) AdvanceHop() {
	if p.HopPos < MaxHops-1 {
		p.HopPos++
		p.Buf[0] = p.HopPos
	}
}

func AdvanceRawHop(pkt []byte) {
	if len(pkt) >= HeaderSize && pkt[0] < MaxHops-1 {
		pkt[0]++
	}
}

func (p *Packet) SetPort(port uint16) {
	p.Port = port
}

func (p *Packet) AppendUserPacket(userID uint32, data []byte) bool {
	subSize := 4 + 2 + len(data)
	if p.wp+subSize > p.BuffSize {
		return false
	}

	binary.BigEndian.PutUint32(p.Buf[p.wp:], userID)
	p.wp += 4

	binary.BigEndian.PutUint16(p.Buf[p.wp:], uint16(len(data)))
	p.wp += 2

	copy(p.Buf[p.wp:], data)
	p.wp += len(data)

	p.PayloadLen = uint16(p.wp - HeaderSize)
	return true
}

func (p *Packet) SerializeHead() {
	b := p.Buf[:HeaderSize]

	b[0] = p.HopPos

	binary.BigEndian.PutUint32(b[1:5], p.HopIP[0])
	binary.BigEndian.PutUint32(b[5:9], p.HopIP[1])
	binary.BigEndian.PutUint32(b[9:13], p.HopIP[2])
	binary.BigEndian.PutUint32(b[13:17], p.HopIP[3])
	binary.BigEndian.PutUint16(b[17:19], p.PayloadLen)
	binary.BigEndian.PutUint16(b[19:21], p.Port)
}

func (p *Packet) TotalBytes() int {
	return HeaderSize + int(p.PayloadLen)
}

func ParseHeader(raw []byte) (*Packet, error) {
	if len(raw) < HeaderSize {
		return nil, ErrInvalidHeader
	}

	p := &Packet{
		Buf: make([]byte, HeaderSize),
	}
	copy(p.Buf, raw[:HeaderSize])

	b := p.Buf[:HeaderSize]

	p.HopPos = b[0]
	p.HopIP[0] = binary.BigEndian.Uint32(b[1:5])
	p.HopIP[1] = binary.BigEndian.Uint32(b[5:9])
	p.HopIP[2] = binary.BigEndian.Uint32(b[9:13])
	p.HopIP[3] = binary.BigEndian.Uint32(b[13:17])
	p.PayloadLen = binary.BigEndian.Uint16(b[17:19])
	p.Port = binary.BigEndian.Uint16(b[19:21])

	return p, nil
}

func Parse(raw []byte, bufferSize int) (*Packet, []SubPacket, error) {
	if len(raw) < HeaderSize {
		return nil, nil, fmt.Errorf("raw data too short")
	}

	p := NewPacket(bufferSize)
	copy(p.Buf, raw)

	b := p.Buf[:HeaderSize]

	p.HopPos = b[0]
	p.HopIP[0] = binary.BigEndian.Uint32(b[1:5])
	p.HopIP[1] = binary.BigEndian.Uint32(b[5:9])
	p.HopIP[2] = binary.BigEndian.Uint32(b[9:13])
	p.HopIP[3] = binary.BigEndian.Uint32(b[13:17])
	p.PayloadLen = binary.BigEndian.Uint16(b[17:19])
	p.Port = binary.BigEndian.Uint16(b[19:21])

	payloadEnd := HeaderSize + int(p.PayloadLen)
	if payloadEnd > len(raw) || payloadEnd > bufferSize {
		return nil, nil, fmt.Errorf("invalid payload length")
	}
	payload := p.Buf[HeaderSize:payloadEnd]

	var subs []SubPacket
	r := bytes.NewReader(payload)

	for {
		var userID uint32
		if err := binary.Read(r, binary.BigEndian, &userID); err != nil {
			break
		}

		var subLen uint16
		if err := binary.Read(r, binary.BigEndian, &subLen); err != nil {
			break
		}

		data := make([]byte, subLen)
		if _, err := r.Read(data); err != nil {
			break
		}

		subs = append(subs, SubPacket{
			UserID: userID,
			Data:   data,
		})
	}

	return p, subs, nil
}

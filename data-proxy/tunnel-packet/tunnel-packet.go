package tunnel_packet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// 包头结构：24字节
// b[0]: HopPos(1) + b[1:17]: HopIP(16) + b[17:19]: PayloadLen(2) + b[19:21]: Port(2) + b[21:24]: padding(3)
const (
	HeaderSize = 24
	MaxHops    = 4 // 总跳数固定 4 跳 (0~3)
)

var ErrInvalidHeader = errors.New("invalid packet header: length too short")

// Packet 多级代理合并分包总包结构
type Packet struct {
	Buf        []byte    // 缓冲区
	BuffSize   int       // 外部传入的缓冲区大小
	HopPos     byte      // 当前所在跳数 0~3
	HopIP      [4]uint32 // 4 跳节点 IPv4
	PayloadLen uint16    // 子报文总长度
	Port       uint16    // 目的端口

	wp int // payload 内部写入游标
}

// SubPacket 单个用户子报文
type SubPacket struct {
	UserID uint32
	Data   []byte
}

// NewPacket 创建一个预分配 5K 的空包
func NewPacket(buffSizes int) *Packet {
	return &Packet{
		Buf: make([]byte, buffSizes),
		wp:  HeaderSize,
	}
}

// SetHopIP 设置第 n 跳 IP
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

// SetHopPos 手动设置当前跳数
func (p *Packet) SetHopPos(pos byte) {
	if pos >= 0 && pos < MaxHops {
		p.HopPos = pos
	}
}

// AdvanceHop HopPos + 1，并写回 Buf
func (p *Packet) AdvanceHop() {
	if p.HopPos < MaxHops-1 {
		p.HopPos++
		p.Buf[0] = p.HopPos
	}
}

// AdvanceRawHop 原始字节 HopPos + 1（直接操作字节数组）
func AdvanceRawHop(pkt []byte) {
	if len(pkt) >= HeaderSize && pkt[0] < MaxHops-1 {
		pkt[0]++
	}
}

// SetPort 设置目的端口
func (p *Packet) SetPort(port uint16) {
	p.Port = port
}

// AppendUserPacket 追加一个用户子包
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

// SerializeHead 将包头结构写回 Buf 前 24 字节
func (p *Packet) SerializeHead() {
	b := p.Buf[:HeaderSize]

	// 0 字节：跳数
	b[0] = p.HopPos

	// 1~17 字节：4个IP
	binary.BigEndian.PutUint32(b[1:5], p.HopIP[0])
	binary.BigEndian.PutUint32(b[5:9], p.HopIP[1])
	binary.BigEndian.PutUint32(b[9:13], p.HopIP[2])
	binary.BigEndian.PutUint32(b[13:17], p.HopIP[3])

	// 17~19 字节：payload 长度
	binary.BigEndian.PutUint16(b[17:19], p.PayloadLen)

	// 19~21 字节：端口
	binary.BigEndian.PutUint16(b[19:21], p.Port)

	// 21~24 字节：reserved padding
}

// TotalBytes 实际使用总长度
func (p *Packet) TotalBytes() int {
	return HeaderSize + int(p.PayloadLen)
}

// ParseHeader 只解析包头（24字节），转发层专用
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

// Parse 解析整包 + 所有子包
func Parse(raw []byte, bufferSize int) (*Packet, []SubPacket, error) {
	if len(raw) < HeaderSize {
		return nil, nil, fmt.Errorf("raw data too short")
	}

	p := NewPacket(bufferSize)
	copy(p.Buf, raw)

	b := p.Buf[:HeaderSize]

	// 解析包头
	p.HopPos = b[0]
	p.HopIP[0] = binary.BigEndian.Uint32(b[1:5])
	p.HopIP[1] = binary.BigEndian.Uint32(b[5:9])
	p.HopIP[2] = binary.BigEndian.Uint32(b[9:13])
	p.HopIP[3] = binary.BigEndian.Uint32(b[13:17])
	p.PayloadLen = binary.BigEndian.Uint16(b[17:19])
	p.Port = binary.BigEndian.Uint16(b[19:21])

	// 解析 payload
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

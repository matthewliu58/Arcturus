package packet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	log "github.com/sirupsen/logrus"
	"net"
)

type Packet struct {
	Length      uint16
	HeaderLen   uint16
	Timestamp   uint32
	PacketType  byte
	Priority    byte
	Property    byte
	HopCounts   byte
	PacketCount byte
	Offsets     []uint16
	Padding     []byte
	PacketID    []uint32
	HopList     []uint32
}

func (p *Packet) Pack() ([]byte, error) {
	if len(p.PacketID) != int(p.PacketCount) {
		return nil, fmt.Errorf("PacketID %d PacketCount %d ", len(p.PacketID), p.PacketCount)
	}

	expectedOffsets := 0
	if p.PacketCount > 1 {
		expectedOffsets = int(p.PacketCount) - 1
	}
	if len(p.Offsets) != expectedOffsets {
		return nil, fmt.Errorf("offsets %d  %d ", len(p.Offsets), expectedOffsets)
	}

	fixedHeaderLen := 2 + 2 + 4 + 1 + 1 + 1 + 1 + 1

	variableLen := len(p.Offsets)*2 + len(p.PacketID)*4 + len(p.HopList)*4

	headerLen := fixedHeaderLen + variableLen

	paddingLen := (4 - (headerLen % 4)) % 4
	p.HeaderLen = uint16(headerLen + paddingLen)

	p.Length = p.Length + p.HeaderLen

	p.Padding = make([]byte, paddingLen)

	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.BigEndian, p.Length); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.HeaderLen); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.Timestamp); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.PacketType); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.Priority); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.Property); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.HopCounts); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, p.PacketCount); err != nil {
		return nil, err
	}

	for _, offset := range p.Offsets {
		if err := binary.Write(&buf, binary.BigEndian, offset); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(&buf, binary.BigEndian, p.Padding); err != nil {
		return nil, err
	}

	for _, id := range p.PacketID {
		if err := binary.Write(&buf, binary.BigEndian, id); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(&buf, binary.BigEndian, p.HopList); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func Unpack(data []byte) (*Packet, error) {
	var packet Packet
	buf := bytes.NewReader(data)

	if err := binary.Read(buf, binary.BigEndian, &packet.Length); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.HeaderLen); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.Timestamp); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.PacketType); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.Priority); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.Property); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.HopCounts); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &packet.PacketCount); err != nil {
		return nil, err
	}

	offsetsCount := 0
	if packet.PacketCount > 1 {
		offsetsCount = int(packet.PacketCount) - 1
	}

	if offsetsCount > 0 {
		packet.Offsets = make([]uint16, offsetsCount)
		for i := 0; i < offsetsCount; i++ {
			var offset uint16
			if err := binary.Read(buf, binary.BigEndian, &offset); err != nil {
				return nil, err
			}
			packet.Offsets[i] = offset
		}
	} else {
		packet.Offsets = []uint16{} //
	}

	fixedHeaderLen := 2 + 2 + 4 + 1 + 1 + 1 + 1 + 1
	currentPos := fixedHeaderLen + len(packet.Offsets)*2
	paddingSize := (4 - (currentPos % 4)) % 4

	if paddingSize > 0 {
		packet.Padding = make([]byte, paddingSize)
		if err := binary.Read(buf, binary.BigEndian, packet.Padding); err != nil {
			return nil, err
		}
	}

	packet.PacketID = make([]uint32, packet.PacketCount)
	for i := 0; i < int(packet.PacketCount); i++ {
		if err := binary.Read(buf, binary.BigEndian, &packet.PacketID[i]); err != nil {
			return nil, err
		}
	}

	remainingBytes := int(packet.HeaderLen) - (fixedHeaderLen + len(packet.Offsets)*2 + paddingSize + len(packet.PacketID)*4)
	hopListCount := remainingBytes / 4

	if hopListCount > 0 {
		packet.HopList = make([]uint32, hopListCount)
		if err := binary.Read(buf, binary.BigEndian, packet.HopList); err != nil {
			return nil, err
		}
	}

	return &packet, nil
}

func NewPacket(hopList []string, packetID uint32) (*Packet, error) {
	var uint32HopList []uint32
	for _, ip := range hopList {
		ipUint32, err := IpToUint32(ip)
		if err != nil {
			return nil, err
		}
		uint32HopList = append(uint32HopList, ipUint32)
	}

	packet := &Packet{
		Length:      50,
		Timestamp:   1617916800,
		PacketType:  1,
		Priority:    0,
		Property:    0,
		HopCounts:   1,
		PacketCount: 1,
		Offsets:     []uint16{},

		PacketID: []uint32{packetID},
		HopList:  uint32HopList,
	}

	return packet, nil
}

func NewMergedPacket(packetIDs []uint32, requestSizes []int, hopList []uint32, packetType byte) *Packet {

	totalSize := 0
	for _, size := range requestSizes {
		totalSize += size
	}

	offsets := CalcRelativeOffsets(requestSizes)

	packet := &Packet{
		Length:      uint16(totalSize),
		Timestamp:   1617916800,
		PacketType:  packetType,
		Priority:    0,
		Property:    0,
		HopCounts:   1,
		PacketCount: byte(len(packetIDs)),
		Offsets:     offsets,
		PacketID:    packetIDs,
		HopList:     hopList,
	}

	return packet
}

func CalcRelativeOffsets(bodySizes []int) []uint16 {

	if len(bodySizes) <= 1 {
		return []uint16{}
	}

	offsets := make([]uint16, len(bodySizes)-1)
	for i := 0; i < len(bodySizes)-1; i++ {

		size := bodySizes[i]
		if size > 65535 {

			log.Warningf("[WARNING]CalcRelativeOffsets, size=%d uint16 65535", size)
			size = 65535
		}
		offsets[i] = uint16(size)
	}

	return offsets
}

func GetRequestPositions(p *Packet, bodyLength int) []int {
	count := int(p.PacketCount)
	positions := make([]int, count+1)

	positions[0] = 0

	currentPos := 0
	for i := 0; i < count-1; i++ {
		currentPos += int(p.Offsets[i])
		positions[i+1] = currentPos
	}

	positions[count] = bodyLength

	return positions
}

func IsCommonBackwardPath(hopList1, hopList2 []uint32) bool {

	if len(hopList1) == 0 || len(hopList2) == 0 {
		return false
	}

	path1 := hopList1[:len(hopList1)-1]
	path2 := hopList2[:len(hopList2)-1]

	if len(path1) != len(path2) {
		return false
	}

	for i := 0; i < len(path1); i++ {
		if path1[i] != path2[i] {
			return false
		}
	}

	return true
}

func IpToUint32(ip string) (uint32, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return 0, fmt.Errorf("invalid IP address")
	}

	ipv4 := parsedIP.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("not an IPv4 address")
	}

	return binary.BigEndian.Uint32(ipv4), nil
}

func Uint32ToIP(ipUint32 uint32) string {
	ip := make([]byte, 4)
	binary.BigEndian.PutUint32(ip, ipUint32)
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

func (p *Packet) GetNextHopIP() (string, bool, error) {

	if int(p.HopCounts) > len(p.HopList) {
		return "", false, fmt.Errorf("HopCounts %d exceeds the length of HopList %d", p.HopCounts, len(p.HopList))
	}

	log.Infof("[PACKET]GetNextHopIP, HopCounts=%d, HopList=%d", p.HopCounts, len(p.HopList))

	var ipAddresses []string
	for _, ipUint32 := range p.HopList {
		ipAddresses = append(ipAddresses, Uint32ToIP(ipUint32))
	}

	nextHopIndex := int(p.HopCounts)

	hasNextHop := nextHopIndex < len(ipAddresses)

	if !hasNextHop {
		log.Printf("[PACKET] : ")
		return "", false, nil // ，false
	}

	nextHopIP := ipAddresses[nextHopIndex]

	isLastHop := nextHopIndex == len(ipAddresses)-1

	log.Infof("[PACKET]GetNextHopIP, IP=%s, isLastHop=%v", nextHopIP, isLastHop)

	return nextHopIP, isLastHop, nil
}

func (p *Packet) GetPreviousHopIP() (string, bool, error) {

	if int(p.HopCounts) > len(p.HopList) {
		return "", false, fmt.Errorf("HopCounts %d exceeds the length of HopList %d", p.HopCounts, len(p.HopList))
	}

	if p.HopCounts == 1 {
		return "", false, nil
	}

	previousHopIndex := int(p.HopCounts) - 2

	if previousHopIndex < 0 || previousHopIndex >= len(p.HopList) {

		if len(p.HopList) > 0 {
			// （Access）
			return Uint32ToIP(p.HopList[0]), true, nil
		}
		return "", false, fmt.Errorf(" %d  (HopList=%d)", previousHopIndex, len(p.HopList))
	}

	var ipAddresses []string
	for _, ipUint32 := range p.HopList {
		ipAddresses = append(ipAddresses, Uint32ToIP(ipUint32))
	}

	previousHopIP := ipAddresses[previousHopIndex]
	return previousHopIP, true, nil
}

func (p *Packet) IncrementHopCounts() {
	p.HopCounts++
}

func (p *Packet) DecrementHopCounts() {
	p.HopCounts--
}

package main

import (
	"data-proxy/backsourcer"
	"data-proxy/disaggregator"
	"data-proxy/tunnel-manager"
	"data-proxy/tunnel-packet"
	"encoding/binary"
	"log/slog"
	"net"
	"strconv"
)

// HandleQUICPacket QUIC 收到包后，唯一入口
func HandleQUICPacket(remoteAddr string, pkt []byte) {
	if len(pkt) < tunnel_packet.HeaderSize {
		return
	}

	header := pkt[:tunnel_packet.HeaderSize]
	port := binary.BigEndian.Uint16(header[19:21]) // Port 在 b[19:21]
	pos := header[0]

	if port != 0 {
		// === 去程：访问源站 ===
		// 判断下一个 hop 是不是源站（pos >= 2 或 下一个 IP 是 0）
		isLastHop := pos >= 2
		if int(pos)+1 < tunnel_packet.MaxHops {
			nextIP := binary.BigEndian.Uint32(header[1+4*(pos+1) : 1+4*(pos+2)])
			if nextIP == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			// 源站：交给 backsourcer
			packet, subs, err := tunnel_packet.Parse(pkt)
			if err != nil || len(subs) == 0 {
				return
			}

			// 源站 IP 在最后一个 hop
			originIPVal := binary.BigEndian.Uint32(header[1+4*(tunnel_packet.MaxHops-1) : 1+4*tunnel_packet.MaxHops])
			originIP := Uint32ToIP(originIPVal)
			originAddr := net.JoinHostPort(originIP.String(), strconv.Itoa(int(port)))

			for _, sub := range subs {
				backsourcer.GlobalBackSourcer.Submit(&backsourcer.BackSourceTask{
					HopIP:      packet.HopIP, // 从 Packet 获取完整路径
					Port:       port,
					UserID:     sub.UserID,
					OriginAddr: originAddr,
					ReqData:    sub.Data,
				})
			}
		} else {
			// 不是源站：转发给下一个 hop，更新 pos
			nextIPVal := binary.BigEndian.Uint32(header[1+4*(pos+1) : 1+4*(pos+2)])
			nextIP := Uint32ToIP(nextIPVal)

			pkt[0] = pos + 1 // 更新 HopPos
			if err := tunnel_manager.TunnelMgr.SendPacket(nil, nextIP, pkt, "quic", slog.Default()); err != nil {
				slog.Error("去程转发失败", "err", err)
			}
		}
	} else {
		// === 返程：返回给用户 ===
		// 判断是不是到目的地了（下一个是 0 或 pos >= 3）
		isLastHop := pos >= 3
		if int(pos)+1 < tunnel_packet.MaxHops {
			nextIP := binary.BigEndian.Uint32(header[1+4*(pos+1) : 1+4*(pos+2)])
			if nextIP == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			// 到目的地：交给 disaggregator
			_, subs, err := tunnel_packet.Parse(pkt)
			if err != nil || len(subs) == 0 {
				return
			}

			for _, sub := range subs {
				disaggregator.GlobalDisagg.Deliver(sub.UserID, sub.Data)
			}
		} else {
			// 没到目的地：转发给下一个 hop，更新 pos
			nextIPVal := binary.BigEndian.Uint32(header[1+4*(pos+1) : 1+4*(pos+2)])
			nextIP := Uint32ToIP(nextIPVal)

			pkt[0] = pos + 1 // 更新 HopPos
			if err := tunnel_manager.TunnelMgr.SendPacket(nil, nextIP, pkt, "quic", slog.Default()); err != nil {
				slog.Error("返程转发失败", "err", err)
			}
		}
	}
}

// Uint32 转 net.IP
func Uint32ToIP(ipUint32 uint32) net.IP {
	return net.IP{byte(ipUint32 >> 24), byte(ipUint32 >> 16), byte(ipUint32 >> 8), byte(ipUint32)}
}

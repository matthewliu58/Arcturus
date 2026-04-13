package main

import (
	"context"
	"data-proxy/backsourcer"
	"data-proxy/disaggregator"
	tunnel_manager "data-proxy/tunnel-manager"
	tunnel_packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"log/slog"
	"net"
	"strconv"
)

// HandleQUICPacket QUIC 收到包后，唯一入口
func HandleQUICPacket(remoteAddr string, pkt []byte, l *slog.Logger) {
	if len(pkt) < tunnel_packet.HeaderSize {
		return
	}

	// 解析包头
	header, err := tunnel_packet.ParseHeader(pkt)
	if err != nil {
		return
	}

	if header.Port != 0 {
		// === 去程：访问源站 ===
		// 判断下一个 hop 是不是源站（pos >= 2 或 下一个 IP 是 0）
		isLastHop := header.HopPos == 2
		if int(header.HopPos)+2 < tunnel_packet.MaxHops {
			if header.HopIP[header.HopPos+2] == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			// 源站：交给 backsourcer
			_, subs, err := tunnel_packet.Parse(pkt, len(pkt))
			if err != nil || len(subs) == 0 {
				return
			}

			// 源站 IP 在最后一个 hop
			originIP := util.Uint32ToIP(header.HopIP[int(header.HopPos)+1])
			originAddr := net.JoinHostPort(originIP.String(), strconv.Itoa(int(header.Port)))

			for _, sub := range subs {
				backsourcer.GlobalBackSourcer.Submit(&backsourcer.BackSourceTask{
					HopIP:      header.HopIP, // 从 Header 获取完整路径
					Port:       header.Port,
					UserID:     sub.UserID,
					OriginAddr: originAddr,
					ReqData:    sub.Data,
				})
			}
		} else {
			// 不是源站：转发给下一个 hop，更新 pos
			nextIP := util.Uint32ToIP(header.HopIP[header.HopPos+1])
			tunnel_packet.AdvanceRawHop(pkt)
			if err := tunnel_manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, "quic", l); err != nil {
				l.Error("去程转发失败", "err", err)
			}
		}
	} else {
		// === 返程：返回给用户 ===
		// 判断是不是到目的地了（下一个是 0 或 pos >= 3）
		isLastHop := header.HopPos == 3
		if int(header.HopPos)+1 < tunnel_packet.MaxHops {
			if header.HopIP[header.HopPos+1] == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			// 到目的地：交给 disaggregator
			_, subs, err := tunnel_packet.Parse(pkt, len(pkt))
			if err != nil || len(subs) == 0 {
				return
			}

			for _, sub := range subs {
				disaggregator.GlobalDisagg.Deliver(sub.UserID, sub.Data)
			}
		} else {
			// 没到目的地：转发给下一个 hop，更新 pos
			nextIP := util.Uint32ToIP(header.HopIP[header.HopPos+1])
			tunnel_packet.AdvanceRawHop(pkt)
			if err := tunnel_manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, "quic", l); err != nil {
				l.Error("返程转发失败", "err", err)
			}
		}
	}
}

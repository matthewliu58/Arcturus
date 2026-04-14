package main

import (
	"context"
	"data-proxy/backsourcer"
	"data-proxy/disaggregator"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"log/slog"
	"net"
	"strconv"
)

func HandleQUICPacket(remoteAddr string, pkt []byte, l *slog.Logger) {
	if len(pkt) < packet.HeaderSize {
		return
	}

	header, err := packet.ParseHeader(pkt)
	if err != nil {
		return
	}

	if header.Port != 0 {
		isLastHop := header.HopPos == 2
		if int(header.HopPos)+2 < packet.MaxHops {
			if header.HopIP[header.HopPos+2] == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			var subs []packet.SubPacket
			_, subs, err = packet.Parse(pkt, len(pkt))
			if err != nil || len(subs) == 0 {
				return
			}

			originIP := util.Uint32ToIP(header.HopIP[int(header.HopPos)+1])
			originAddr := net.JoinHostPort(originIP.String(), strconv.Itoa(int(header.Port)))

			for _, sub := range subs {
				backsourcer.BackSourcerMap["tcp"].Submit(
					&backsourcer.BackSourceTask{
						HopIP:      header.HopIP,
						Port:       header.Port,
						UserID:     sub.UserID,
						OriginAddr: originAddr,
						ReqData:    sub.Data,
					})
			}
		} else {
			nextIP := util.Uint32ToIP(header.HopIP[header.HopPos+1])
			packet.AdvanceRawHop(pkt)
			if err = manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, "quic", l); err != nil {
				l.Error("去程转发失败", "err", err)
			}
		}
	} else {
		isLastHop := header.HopPos == 3
		if int(header.HopPos)+1 < packet.MaxHops {
			if header.HopIP[header.HopPos+1] == 0 {
				isLastHop = true
			}
		}

		if isLastHop {
			var subs []packet.SubPacket
			_, subs, err = packet.Parse(pkt, len(pkt))
			if err != nil || len(subs) == 0 {
				return
			}

			for _, sub := range subs {
				disaggregator.GlobalDisagg.Deliver(sub.UserID, sub.Data)
			}
		} else {
			nextIP := util.Uint32ToIP(header.HopIP[header.HopPos+1])
			packet.AdvanceRawHop(pkt)
			if err = manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, "quic", l); err != nil {
				l.Error("返程转发失败", "err", err)
			}
		}
	}
}

package main

import (
	"context"
	"data-proxy/backsourcer"
	"data-proxy/disaggregator"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"fmt"
	"log/slog"
	"net"
	"strconv"
)

func HandleQUICPacket(remoteAddr string, pkt []byte, l *slog.Logger) {
	if len(pkt) < packet.HeaderSize {
		l.Error("Invalid packet length", slog.Any("remoteAddr", remoteAddr), slog.Any("pktLen", len(pkt)))
		return
	}

	header, err := packet.ParseHeader(pkt)
	if err != nil {
		l.Error("Failed to parse packet", slog.String("remoteAddr", remoteAddr), slog.Any("err", err))
		return
	}

	hopIPStr := fmt.Sprintf("%d.%d.%d.%d", header.HopIP[0]>>24,
		header.HopIP[0]>>16&0xFF, header.HopIP[0]>>8&0xFF, header.HopIP[0]&0xFF)
	l.Info("Received QUIC packet", slog.String("remoteAddr", remoteAddr), slog.String("HopIP", hopIPStr),
		slog.Any("port", header.Port), slog.Any("HopPos", header.HopPos), slog.Any("pktLen", len(pkt)))

	if header.Port != 0 {

		protocal := "tcp"
		if header.Protocol == 17 {
			protocal = "udp"
		}

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
			if originIP.String() == "0.0.0.0" {
				l.Error("Origin IP is 0.0.0.0", slog.String("remoteAddr", remoteAddr), slog.Any("pktLen", len(pkt)))
				return
			}
			originAddr := net.JoinHostPort(originIP.String(), strconv.Itoa(int(header.Port)))

			for _, sub := range subs {
				l.Debug("", slog.Any("HopIP", header.HopIP), slog.Any("UserID", sub.UserID), slog.String("ReqData", string(sub.Data)))
				backsourcer.BackSourcerMap[protocal].Submit(
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
			if err = manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, nextIP.String(), l); err != nil {
				l.Error("Outbound forwarding failed", "err", err)
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
			if err = manager.TunnelMgr.SendPacket(context.Background(), nextIP, pkt, nextIP.String(), l); err != nil {
				l.Error("Return forwarding failed", "err", err)
			}
		}
	}
}

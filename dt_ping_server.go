package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// 假设这些是你已有的常量和结构体
const (
	UDP_PORT      = 0xDA27
	ICMP_PROTOCOL = 1
	ICMP_ECHO     = 8
	ICMP_REPLY    = 0
)

type ICMPPacket struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	ID       uint16
	Seq      uint16
	Payload  []byte
}

func (p *ICMPPacket) Pack() []byte {
	buf := make([]byte, 8+len(p.Payload))
	buf[0] = p.Type
	buf[1] = p.Code
	binary.BigEndian.PutUint16(buf[2:], p.Checksum)
	binary.BigEndian.PutUint16(buf[4:], p.ID)
	binary.BigEndian.PutUint16(buf[6:], p.Seq)
	copy(buf[8:], p.Payload)
	return buf
}

func (p *ICMPPacket) CalculateChecksum() uint16 {
	buf := p.Pack()
	var sum uint32
	for i := 0; i < len(buf)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(buf[i:]))
	}
	if len(buf)%2 == 1 {
		sum += uint32(buf[len(buf)-1]) << 8
	}
	for (sum >> 16) != 0 {
		sum = (sum >> 16) + (sum & 0xFFFF)
	}
	return ^uint16(sum)
}

type DARTHeader struct {
	Version       uint8
	UpperProtocol uint8
	DstLen        uint8
	SrcLen        uint8
	DstFQDN       []byte
	SrcFQDN       []byte
}

func (h *DARTHeader) Pack() []byte {
	buf := []byte{h.Version, h.UpperProtocol, h.DstLen, h.SrcLen}
	buf = append(buf, h.DstFQDN...)
	buf = append(buf, h.SrcFQDN...)
	return buf
}

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root")
	}

	// 使用标准UDP套接字（系统自动添加/解析UDP报头）
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: UDP_PORT})
	if err != nil {
		log.Fatalf("Failed to listen on UDP: %v", err)
	}
	defer udpConn.Close()

	// 处理 Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\nServer shutting down...")
		udpConn.Close()
		os.Exit(0)
	}()

	fmt.Println("DART Ping responder started. Waiting for requests...")

	buf := make([]byte, 1500)
	for {
		n, srcAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Read error: %v", err)
			continue
		}
		if n < 8 {
			continue
		}

		// 解析 DART 头
		if buf[0] != 1 || buf[1] != ICMP_PROTOCOL {
			continue
		}
		dstLen := int(buf[2])
		srcLen := int(buf[3])
		if n < 4+dstLen+srcLen+8 {
			continue
		}
		dstFQDN := string(buf[4 : 4+dstLen])
		srcFQDN := string(buf[4+dstLen : 4+dstLen+srcLen])
		icmpStart := 4 + dstLen + srcLen

		if buf[icmpStart] != ICMP_ECHO || buf[icmpStart+1] != 0 {
			continue
		}

		// 构造ICMP响应
		reply := &ICMPPacket{
			Type:    ICMP_REPLY,
			Code:    0,
			ID:      binary.BigEndian.Uint16(buf[icmpStart+4 : icmpStart+6]),
			Seq:     binary.BigEndian.Uint16(buf[icmpStart+6 : icmpStart+8]),
			Payload: buf[icmpStart+8 : n],
		}
		reply.Checksum = reply.CalculateChecksum()

		// 构造 DART 头（交换源/目标 FQDN）
		dartHeader := &DARTHeader{
			Version:       1,
			UpperProtocol: ICMP_PROTOCOL,
			DstLen:        uint8(len(srcFQDN)),
			SrcLen:        uint8(len(dstFQDN)),
			DstFQDN:       []byte(srcFQDN),
			SrcFQDN:       []byte(dstFQDN),
		}
		dartPacket := dartHeader.Pack()
		icmpPacket := reply.Pack()
		fullPayload := append(dartPacket, icmpPacket...)

		// 使用 udpConn 发回响应，自动使用UDP_PORT为源端口
		_, err = udpConn.WriteToUDP(fullPayload, srcAddr)
		if err != nil {
			log.Printf("Failed to send response: %v", err)
		} else {
			log.Printf("Responded to ping from %s (%s), size: %d", srcFQDN, srcAddr.String(), len(fullPayload))
		}
	}
}

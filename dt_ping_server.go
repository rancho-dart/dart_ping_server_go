package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	// "time"
)

const (
	DART_PROTOCOL = 254
	ICMP_PROTOCOL = 1
	ICMP_ECHO     = 8
	ICMP_REPLY    = 0
)

type DARTHeader struct {
	Version       uint8
	UpperProtocol uint8
	DstLen        uint8
	SrcLen        uint8
	DstFQDN       []byte
	SrcFQDN       []byte
}

type ICMPPacket struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	ID       uint16
	Seq      uint16
	Payload  []byte
}

func (h *DARTHeader) Pack() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, h.Version)
	binary.Write(buf, binary.BigEndian, h.UpperProtocol)
	binary.Write(buf, binary.BigEndian, h.DstLen)
	binary.Write(buf, binary.BigEndian, h.SrcLen)
	buf.Write(h.DstFQDN)
	buf.Write(h.SrcFQDN)
	return buf.Bytes()
}

func (p *ICMPPacket) CalculateChecksum() uint16 {
	p.Checksum = 0
	var sum uint32

	data := p.Pack()
	for i := 0; i < len(data); i += 2 {
		if i+1 < len(data) {
			sum += uint32(data[i+1])<<8 | uint32(data[i])
		} else {
			sum += uint32(data[i]) << 8
		}
	}

	sum = (sum >> 16) + (sum & 0xffff)
	sum += sum >> 16

	// 交换Checksum的高低字节顺序
	checksum := uint16(^sum)
	return (checksum<<8)&0xff00 | (checksum>>8)&0x00ff
}

func (p *ICMPPacket) Pack() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, p.Type)
	binary.Write(buf, binary.BigEndian, p.Code)
	binary.Write(buf, binary.BigEndian, p.Checksum)
	binary.Write(buf, binary.BigEndian, p.ID)
	binary.Write(buf, binary.BigEndian, p.Seq)
	buf.Write(p.Payload)
	return buf.Bytes()
}

func main() {
	// 需要root权限运行
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root")
	}

	// 创建原始套接字监听DART协议
	conn, err := net.ListenPacket(fmt.Sprintf("ip4:%d", DART_PROTOCOL), "0.0.0.0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer conn.Close()

	// 处理Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\nServer shutting down...")
		conn.Close()
		os.Exit(0)
	}()

	fmt.Println("DART Ping responder started. Waiting for requests...")

	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("Read error: %v", err)
			continue
		}

		// 解析DART头
		dartStart := 0
		if buf[dartStart] != 1 || buf[dartStart+1] != ICMP_PROTOCOL {
			continue // 版本或协议不匹配
		}

		dstLen := int(buf[dartStart+2])
		srcLen := int(buf[dartStart+3])
		dstFQDN := string(buf[dartStart+4 : dartStart+4+dstLen])
		srcFQDN := string(buf[dartStart+4+dstLen : dartStart+4+dstLen+srcLen])

		// 解析ICMP请求
		icmpStart := dartStart + 4 + dstLen + srcLen
		if n < icmpStart+8 {
			continue
		}

		if buf[icmpStart] == ICMP_ECHO && buf[icmpStart+1] == 0 {
			// 构造ICMP响应
			reply := &ICMPPacket{
				Type:    ICMP_REPLY,
				Code:    0,
				ID:      binary.BigEndian.Uint16(buf[icmpStart+4 : icmpStart+6]),
				Seq:     binary.BigEndian.Uint16(buf[icmpStart+6 : icmpStart+8]),
				Payload: buf[icmpStart+8 : n],
			}
			reply.Checksum = reply.CalculateChecksum()

			// 构造DART头(交换源和目标)
			dartHeader := &DARTHeader{
				Version:       1,
				UpperProtocol: ICMP_PROTOCOL,
				DstLen:        uint8(len(srcFQDN)),
				SrcLen:        uint8(len(dstFQDN)),
				DstFQDN:       []byte(srcFQDN),
				SrcFQDN:       []byte(dstFQDN),
			}

			// 组装完整响应包
			packet := append(dartHeader.Pack(), reply.Pack()...)

			// 发送响应
			_, err = conn.WriteTo(packet, addr)
			if err != nil {
				log.Printf("Failed to send response: %v", err)
			} else {
				log.Printf("Responded to ping from %s (%s)", srcFQDN, addr.String())
			}
		}
	}
}

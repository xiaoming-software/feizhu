// Package socks5 实现本地 SOCKS5 子集（无认证、仅 CONNECT），由上层提供经 TLS 的拨号函数。
// 目标地址仅出现在 dial() 内并最终以 tunnel.TypeDialReq 发往 feizhu-server，链路上为 TLS 密文。
package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// DialFunc 建立到目标 host:port 的已就绪连接（例如 *tls.Conn 已完成 ClientHandshake）。
type DialFunc func(host string, port uint16) (net.Conn, error)

// Serve 处理一条客户端 TCP 连接：完成 SOCKS5 握手后调用 dial，再双向转发。
func Serve(c net.Conn, dial DialFunc) error {
	defer c.Close()

	buf := make([]byte, 260)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return err
	}
	if buf[0] != 5 {
		return errors.New("socks5: 不支持的协议版本")
	}
	nmeth := int(buf[1])
	if nmeth < 1 || nmeth > 255 {
		return errors.New("socks5: 非法 nmethods")
	}
	if _, err := io.ReadFull(c, buf[:nmeth]); err != nil {
		return err
	}
	noAuth := false
	for i := 0; i < nmeth; i++ {
		if buf[i] == 0 {
			noAuth = true
			break
		}
	}
	if !noAuth {
		_, _ = c.Write([]byte{5, 0xff})
		return errors.New("socks5: 需要无认证(0x00)方法")
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return err
	}

	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return err
	}
	if buf[0] != 5 {
		return errors.New("socks5: 请求版本错误")
	}
	cmd := buf[1]
	if cmd != 1 {
		_ = sendRep(c, 0x07) // command not supported
		return fmt.Errorf("socks5: 不支持的 CMD %d（仅支持 CONNECT）", cmd)
	}
	atyp := buf[3]
	var host string
	switch atyp {
	case 1:
		if _, err := io.ReadFull(c, buf[:4]); err != nil {
			return err
		}
		host = net.IP(buf[:4]).String()
	case 3:
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			return err
		}
		l := int(buf[0])
		if l <= 0 || l > 253 {
			_ = sendRep(c, 0x01)
			return errors.New("socks5: 域名长度非法")
		}
		if _, err := io.ReadFull(c, buf[:l]); err != nil {
			return err
		}
		host = string(buf[:l])
	case 4:
		if _, err := io.ReadFull(c, buf[:16]); err != nil {
			return err
		}
		host = net.IP(buf[:16]).String()
	default:
		_ = sendRep(c, 0x08)
		return errors.New("socks5: 不支持的地址类型")
	}
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return err
	}
	port := binary.BigEndian.Uint16(buf[:2])

	rc, err := dial(host, port)
	if err != nil {
		_ = sendRep(c, repForDialErr(err))
		return err
	}
	defer rc.Close()

	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(rc, c)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(c, rc)
		errc <- err
	}()
	<-errc
	return nil
}

func sendRep(c net.Conn, rep byte) error {
	_, err := c.Write([]byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}

func repForDialErr(err error) byte {
	// RFC 1928: 0x05 Connection refused, 0x04 host unreachable, 0x02 not allowed, 0x01 general failure
	_ = err
	return 0x05
}

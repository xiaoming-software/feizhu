// Package tunnel 定义 feizhu 在「已与对端建立的 TLS 会话」之上的控制帧协议。
//
// 安全约定（必读）：
//   - feizhu-client 必须先对 feizhu-server 的底层 TCP 完成 TLS 握手（*tls.Conn.Handshake），
//     再在同一连接上调用 ClientHandshake；WriteFrame 的字节会作为 TLS「应用数据」发出，
//     链路上对外部监听者表现为密文，密码与目标站点 host/port 不会以明文出现在 TCP 载荷里。
//   - feizhu-server 必须使用 tls.Listener Accept 得到的连接再调用 ServerHandshake。
//
// 仍可能以明文出现在 TLS 握手里的字段（与目标站点无关）：
//   - 例如 ClientHello 中的 SNI，通常只暴露「代理服务器」的主机名；被访问网站域名在握手之后的
//     加密应用数据里。若需对中间人隐藏代理域名，需要 ECH 等额外机制，不在本包范围内。
package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// 帧头：3 字节魔数 + 1 字节类型 + 4 字节大端长度（与负载分离，避免与魔数字节混淆）
const frameMagic = "FZ1"

// 控制帧类型（在 TLS 连接建立后、进入纯转发前使用）
const (
	TypeAuthReq   = 0x01
	TypeAuthOK    = 0x02
	TypeAuthErr   = 0x03
	TypeDialReq   = 0x04
	TypeDialOK    = 0x05
	TypeDialErr   = 0x06
	TypeProbeReq  = 0x0a // 仅校验密码/会话，不要求 DIAL；用于客户端启动自检
	TypeProbeOK   = 0x0b
)

var (
	ErrAuthFailed = errors.New("tunnel: authentication failed")
	ErrDialFailed = errors.New("tunnel: dial to target failed")
)

// WriteFrame 写入一帧：魔数 + 类型 + u32 大端长度 + 负载
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > 1<<20 {
		return fmt.Errorf("tunnel: payload too large")
	}
	var hdr [8]byte
	copy(hdr[0:], frameMagic)
	hdr[3] = typ
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadFrame 读取一帧，返回类型与负载
func ReadFrame(r io.Reader) (typ byte, payload []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	if string(hdr[0:3]) != frameMagic || hdr[3] == 0 {
		return 0, nil, fmt.Errorf("tunnel: bad magic")
	}
	typ = hdr[3]
	n := binary.BigEndian.Uint32(hdr[4:])
	if n > 1<<20 {
		return 0, nil, fmt.Errorf("tunnel: frame too large")
	}
	if n == 0 {
		return typ, nil, nil
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return typ, payload, nil
}

func setDeadline(c net.Conn, t time.Time) (reset func(), err error) {
	if err := c.SetDeadline(t); err != nil {
		return nil, err
	}
	return func() { _ = c.SetDeadline(time.Time{}) }, nil
}

// ClientVerifyLogin 在 TLS 连接上完成认证并发送探测帧，用于启动时确认密码与服务端一致。
func ClientVerifyLogin(c net.Conn, password string, deadline time.Time) error {
	reset, err := setDeadline(c, deadline)
	if err != nil {
		return err
	}
	defer reset()

	if err := clientSendPasswordAndReadAuthOK(c, password); err != nil {
		return err
	}
	if err := WriteFrame(c, TypeProbeReq, nil); err != nil {
		return err
	}
	typ, body, err := ReadFrame(c)
	if err != nil {
		return err
	}
	if typ != TypeProbeOK {
		return fmt.Errorf("tunnel: unexpected probe response %d %q", typ, body)
	}
	return nil
}

func clientSendPasswordAndReadAuthOK(c net.Conn, password string) error {
	if err := WriteFrame(c, TypeAuthReq, []byte(password)); err != nil {
		return err
	}
	typ, body, err := ReadFrame(c)
	if err != nil {
		return err
	}
	switch typ {
	case TypeAuthOK:
		return nil
	case TypeAuthErr:
		return fmt.Errorf("%w: %s", ErrAuthFailed, string(body))
	default:
		return fmt.Errorf("tunnel: unexpected auth response %d", typ)
	}
}

// ClientHandshake 在已建立的 TLS 连接上完成认证与 DIAL。
// 参数 c 必须为已完成 TLS 握手的 *tls.Conn（或等价封装），否则密码与目标将失去链路机密性。
func ClientHandshake(c net.Conn, password, host string, port uint16, deadline time.Time) error {
	reset, err := setDeadline(c, deadline)
	if err != nil {
		return err
	}
	defer reset()

	if err := clientSendPasswordAndReadAuthOK(c, password); err != nil {
		return err
	}

	dialPayload := make([]byte, 2+len(host))
	binary.BigEndian.PutUint16(dialPayload[0:2], port)
	copy(dialPayload[2:], host)
	if err := WriteFrame(c, TypeDialReq, dialPayload); err != nil {
		return err
	}
	typ, body, err := ReadFrame(c)
	if err != nil {
		return err
	}
	switch typ {
	case TypeDialOK:
		return nil
	case TypeDialErr:
		return fmt.Errorf("%w: %s", ErrDialFailed, string(body))
	default:
		return fmt.Errorf("tunnel: unexpected dial response %d", typ)
	}
}

// ServerHandshake 在 tls.Listener 交付的连接上读取认证，以及随后的「探测」或「DIAL」帧。
// 若客户端发送 TypeProbeReq，则 probeOnly==true 且 target==nil，表示仅登录校验、无转发目标。
// 参数 c 必须来自 TLS（例如 *tls.Conn），与 ClientHandshake 对称。
func ServerHandshake(c net.Conn, expectPassword string, deadline time.Time) (target net.Conn, probeOnly bool, err error) {
	reset, err := setDeadline(c, deadline)
	if err != nil {
		return nil, false, err
	}
	defer reset()

	typ, body, err := ReadFrame(c)
	if err != nil {
		return nil, false, err
	}
	if typ != TypeAuthReq {
		_ = WriteFrame(c, TypeAuthErr, []byte("need auth first"))
		return nil, false, fmt.Errorf("tunnel: expected auth frame")
	}
	if string(body) != expectPassword {
		_ = WriteFrame(c, TypeAuthErr, []byte("invalid password"))
		return nil, false, ErrAuthFailed
	}
	if err := WriteFrame(c, TypeAuthOK, nil); err != nil {
		return nil, false, err
	}

	typ, body, err = ReadFrame(c)
	if err != nil {
		return nil, false, err
	}
	switch typ {
	case TypeProbeReq:
		if err := WriteFrame(c, TypeProbeOK, nil); err != nil {
			return nil, true, err
		}
		return nil, true, nil
	case TypeDialReq:
		if len(body) < 2 {
			_ = WriteFrame(c, TypeDialErr, []byte("bad dial request"))
			return nil, false, fmt.Errorf("tunnel: bad dial frame")
		}
	default:
		_ = WriteFrame(c, TypeDialErr, []byte("expected dial or probe"))
		return nil, false, fmt.Errorf("tunnel: unexpected frame %d", typ)
	}

	port := binary.BigEndian.Uint16(body[0:2])
	host := string(body[2:])
	if host == "" {
		_ = WriteFrame(c, TypeDialErr, []byte("empty host"))
		return nil, false, fmt.Errorf("tunnel: empty host")
	}

	d := net.Dialer{Timeout: 15 * time.Second}
	target, err = d.Dial("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		_ = WriteFrame(c, TypeDialErr, []byte(err.Error()))
		return nil, false, err
	}
	if err := WriteFrame(c, TypeDialOK, nil); err != nil {
		target.Close()
		return nil, false, err
	}
	return target, false, nil
}

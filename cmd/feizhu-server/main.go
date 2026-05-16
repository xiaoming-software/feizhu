package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/feizhu/feizhu/internal/tlscert"
	"github.com/feizhu/feizhu/internal/tunnel"
)

// 已通过密码认证且会话尚未结束的连接数（含「仅探测」与正在转发的隧道）
var authedSessions atomic.Int64

func main() {
	listen := flag.String("listen", ":8443", "监听地址，例如 :8443")
	password := flag.String("password", "", "与客户端一致的密码（必填）")
	certFile := flag.String("cert", "", "TLS 证书 PEM 路径；与 -key 同时指定则使用文件证书")
	keyFile := flag.String("key", "", "TLS 私钥 PEM 路径")
	flag.Parse()

	if *password == "" {
		log.Fatal("请设置 -password")
	}

	var tlsCfg *tls.Config
	var err error
	if *certFile != "" && *keyFile != "" {
		cert, err2 := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err2 != nil {
			log.Fatalf("加载证书失败: %v", err2)
		}
		tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	} else {
		tlsCfg, err = tlscert.ServerTLSConfig()
		if err != nil {
			log.Fatalf("生成自签名证书失败: %v", err)
		}
		log.Println("未指定 -cert/-key，使用内置自签名证书；客户端请使用 -tls-insecure 或导入 CA")
	}

	raw, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	// 控制帧在 TLS 应用层读写；Accept 得到的是 *tls.Conn，再交给 tunnel.ServerHandshake。
	ln := tls.NewListener(raw, tlsCfg)
	log.Printf("feizhu-server 监听 %s (TLS)", *listen)

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(c, *password)
	}
}

func handle(client net.Conn, password string) {
	peer := client.RemoteAddr().String()
	defer client.Close()

	log.Printf("[TLS] 新连接 peer=%s", peer)

	deadline := time.Now().Add(30 * time.Second)
	target, probeOnly, err := tunnel.ServerHandshake(client, password, deadline)
	if err != nil {
		if errors.Is(err, tunnel.ErrAuthFailed) {
			log.Printf("[握手] 认证失败 peer=%s（密码不匹配或协议错误）", peer)
		} else {
			log.Printf("[握手] 失败 peer=%s err=%v", peer, err)
		}
		return
	}

	n := authedSessions.Add(1)
	log.Printf("[握手] 认证成功 peer=%s 当前已认证会话数=%d", peer, n)

	defer func() {
		left := authedSessions.Add(-1)
		log.Printf("[会话结束] peer=%s 剩余已认证会话数=%d", peer, left)
	}()

	if probeOnly {
		log.Printf("[握手] 登录探测完成(无目标转发) peer=%s", peer)
		return
	}

	defer target.Close()
	log.Printf("[隧道] 开始转发 peer=%s -> 目标=%s", peer, target.RemoteAddr().String())

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(target, client)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(client, target)
		errc <- err
	}()
	<-errc

	log.Printf("[隧道] 转发结束 peer=%s -> 目标=%s", peer, target.RemoteAddr().String())
}

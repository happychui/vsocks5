package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/456vv/vconn"
	"github.com/456vv/vconnpool/v2"
	"github.com/happychui/vsocks5"
	"golang.org/x/crypto/ssh"
)

var (
	flog           = flag.String("log", "", "日志文件(默认留空在控制台显示日志)  (format \"./vsocks5.txt\")")
	fuser          = flag.String("user", "", "用户名")
	fpwd           = flag.String("pwd", "", "密码")
	faddr          = flag.String("addr", "", "代理服务器地 (format \"0.0.0.0:1080\")")
	fproxy         = flag.String("proxy", "", "代理服务器的上级代理IP地址 (format \"https://admin:admin@11.22.33.44:8888\" or \"ssh://admin:admin@11.22.33.44:22\")")
	fdataBufioSize = flag.Int("dataBufioSize", 1024*10, "代理数据交换缓冲区大小，单位字节")
)

func main() {
	vsocks5.GlobalKey = "1234567890123456"
	flag.Parse()
	if flag.NFlag() == 0 {
		flag.PrintDefaults()
		fmt.Println("\r\n命令行例子：vsocks5 -addr 0.0.0.0:1080")
		return
	}
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	var out io.Writer = os.Stdout
	if *flog != "" {
		file, err := os.OpenFile(*flog, os.O_CREATE|os.O_RDWR, 0o777)
		if err != nil {
			fmt.Println("日志文件错误：", err)
			return
		}
		out = file
	}

	handle := &vsocks5.DefaultHandle{
		BuffSize: *fdataBufioSize,
	}
	s5 := vsocks5.Server{
		Supported: []byte{vsocks5.CmdConnect, vsocks5.CmdUDP},
		Addr:      *faddr,
		Handle:    handle,
		ErrorLog:  log.New(out, "", log.Lshortfile|log.LstdFlags),
	}
	if *fuser != "" {
		s5.Method = vsocks5.MethodUsernamePassword
		s5.Auth = func(username, password string) bool {
			fmt.Println(username, password, *fuser, *fpwd)
			return username == *fuser && password == *fpwd
		}
	}
	if *fproxy != "" {
		u, err := url.Parse(*fproxy)
		if err != nil {
			fmt.Println("代理地址格式错误：", err)
			return
		}
		var puser, ppwd string
		if u.User != nil {
			puser = u.User.Username()
			ppwd, _ = u.User.Password()
		}
		switch u.Scheme {
		case "ssh":
			// ssh代理
			config := &ssh.ClientConfig{
				User: puser,
				Auth: []ssh.AuthMethod{
					ssh.Password(ppwd),
				},
				HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
					log.Println(hostname, remote, key)
					return nil
				},
				HostKeyAlgorithms: []string{
					ssh.KeyAlgoRSA,
					ssh.KeyAlgoDSA,
					ssh.KeyAlgoECDSA256,
					ssh.KeyAlgoSKECDSA256,
					ssh.KeyAlgoECDSA384,
					ssh.KeyAlgoECDSA521,
					ssh.KeyAlgoED25519,
					ssh.KeyAlgoSKED25519,
				},
				Timeout: 10 * time.Second,
			}

			var (
				sshConnect bool
				dialMux    sync.Mutex
				sshConn    net.Conn
				client     *ssh.Client
			)
			sshReconn := func() error {
				dialMux.Lock()
				defer dialMux.Unlock()
				if sshConnect {
					return nil
				}

				sshConn, client, err = sshDial("tcp", u.Host, config)
				if err != nil {
					return err
				}
				sshConnect = true
				go func() {
					if cn, ok := sshConn.(vconn.CloseNotifier); ok {
						select {
						case err := <-cn.CloseNotify():
							log.Println(err)
							client.Close()
							sshConnect = false
						}
					}
				}()
				return nil
			}
			err = sshReconn()
			if err != nil {
				fmt.Println("代理拨号错误: ", err)
				return
			}
			defer func() {
				client.Close()
			}()
			handle.Dialer.Dial = func(network, address string) (net.Conn, error) {
				if !sshConnect {
					err := sshReconn()
					if err != nil {
						return nil, err
					}
				}
				return client.Dial(network, address)
			}
		case "socks5":
			// socks5代理
			s5Client := &vsocks5.Client{
				Username: puser,
				Password: ppwd,
				Server:   u.Host,
			}
			handle.Dialer.Dial = func(network, address string) (net.Conn, error) {
				return s5Client.Dial(network, address)
			}
		case "https", "http":
			connPool := &vconnpool.ConnPool{
				Dialer: &net.Dialer{
					Timeout:   5 * time.Second,
					DualStack: true,
				},
				IdeTimeout: time.Minute,
			}

			handle.Dialer.Dial = func(network, address string) (net.Conn, error) {
				pconn, err := connPool.Dial(network, u.Host)
				if err != nil {
					return nil, err
				}
				if u.Scheme == "http" {
					return pconn, err
				}

				var pauth string
				if puser != "" {
					pauth = "\nProxy-Authorization: Basic " + basicAuth(puser, ppwd)
				}
				pconn.Write([]byte(fmt.Sprintf("CONNECT %[1]s HTTP/1.1\r\nHost: %[1]s%s\r\n\r\n", address, pauth)))

				resultStatus200 := []byte("HTTP/1.1 200 Connection established\r\n\r\n")
				p := make([]byte, 1024)
				n, err := pconn.Read(p)
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(resultStatus200, p[:n]) {
					pconn.Close()
					return nil, errors.New("https proxy not support")
				}
				return pconn, err
			}
		default:
			fmt.Printf("暂时不支持 %s 协议代理！\n", u.Scheme)
			return
		}
	}
	defer s5.Close()
	err := s5.ListenAndServe()
	if err != nil {
		log.Printf("vsocks5-Error：%s", err)
	}
}

func sshDial(network, addr string, config *ssh.ClientConfig) (net.Conn, *ssh.Client, error) {
	conn, err := net.DialTimeout(network, addr, config.Timeout)
	if err != nil {
		return nil, nil, err
	}

	conn = vconn.NewConn(conn)
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, nil, err
	}

	return conn, ssh.NewClient(c, chans, reqs), nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

package ss

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/ccsexyz/utils"
	"crypto/tls"
	"encoding/binary"
)

type ObfsConn struct {
	RemainConn
	resp     bool
	req      bool
	chunkLen int
	pool     *ConnPool
	eos      bool // end of stream
	lock     sync.Mutex
	rlock    sync.Mutex
	wlock    sync.Mutex
	destroy  bool
}

func (c *ObfsConn) Close() (err error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.destroy {
		return
	}
	c.destroy = true
	if c.pool == nil || c.req || c.resp {
		err = c.RemainConn.Close()
		return
	}
	c.wlock.Lock()
	defer c.wlock.Unlock()
	_, err = c.write(nil)
	if err != nil {
		err = c.RemainConn.Close()
		return
	}
	c.SetReadDeadline(time.Now())
	c.rlock.Lock()
	c.SetReadDeadline(time.Time{})
	defer c.rlock.Unlock()
	buf := utils.GetBuf(buffersize)
	defer utils.PutBuf(buf)
	for !c.eos {
		_, err = c.readInLock(buf)
		if err != nil {
			if c.eos {
				break
			} else {
				err = c.RemainConn.Close()
				return
			}
		}
	}
	err = c.pool.Put(&ObfsConn{
		RemainConn: c.RemainConn,
		pool:       c.pool,
	})
	if err != nil {
		err = c.RemainConn.Close()
	}
	return
}

func (c *ObfsConn) write(b []byte) (n int, err error) {
	n = len(b)
	defer func() {
		if err != nil {
			n = 0
		}
	}()
	wbuf := make([]byte, n+16)
	length := copy(wbuf, []byte(fmt.Sprintf("%x\r\n", n)))
	copy(wbuf[length:], b)
	length += n
	wbuf[length] = '\r'
	wbuf[length+1] = '\n'
	_, err = c.RemainConn.Write(wbuf[:length+2])
	return
}

func (c *ObfsConn) Write(b []byte) (n int, err error) {
	if len(b) == 0 {
		return c.RemainConn.Write(b)
	}
	c.wlock.Lock()
	defer c.wlock.Unlock()
	if c.destroy {
		err = fmt.Errorf("write to closed connection")
		return
	}
	n, err = c.write(b)
	return
}

func (c *ObfsConn) WriteBuffers(b [][]byte) (n int, err error) {
	c.wlock.Lock()
	defer c.wlock.Unlock()
	if c.destroy {
		err = fmt.Errorf("write to closed connection")
		return
	}
	for _, v := range b {
		n += len(v)
	}
	wbuf := make([]byte, 16)
	length := copy(wbuf, []byte(fmt.Sprintf("%x\r\n", n)))
	bufs := make([][]byte, len(b)+2)
	bufs[0] = wbuf[:length]
	copy(bufs[1:], b)
	bufs[1+len(b)] = []byte{'\r', '\n'}
	_, err = c.RemainConn.WriteBuffers(bufs)
	if err != nil {
		n = 0
	}
	return
}

func (c *ObfsConn) readObfsHeader(b []byte) (n int, err error) {
	buf := utils.GetBuf(buffersize)
	defer utils.PutBuf(buf)
	n, err = c.RemainConn.Read(buf)
	if err != nil {
		return
	}
	if n == 0 {
		err = fmt.Errorf("short read")
		return
	}
	parser := utils.NewHTTPHeaderParser(utils.GetBuf(buffersize))
	defer utils.PutBuf(parser.GetBuf())
	ok, err := parser.Read(buf[:n])
	if err != nil {
		return
	}
	if !ok {
		err = fmt.Errorf("unexpected obfs header from %s", c.RemoteAddr().String())
		return
	}
	c.resp = false
	c.req = false
	remain := buf[parser.HeaderLen():n]
	if len(remain) != 0 {
		n = copy(b, remain)
		if n < len(remain) {
			c.remain = append(c.remain, remain[n:]...)
		}
	} else {
		n = 0
	}
	return
}

func (c *ObfsConn) doRead(b []byte) (n int, err error) {
	if c.req || c.resp {
		n, err = c.readObfsHeader(b)
		if err != nil || n != 0 {
			return
		}
	}
	return c.RemainConn.Read(b)
}

func (c *ObfsConn) readInLock(b []byte) (n int, err error) {
	if len(b) == 0 {
		return
	}
	if c.chunkLen <= 2 && c.chunkLen > 0 {
		_, err = c.doRead(b[:c.chunkLen])
		if err != nil {
			return
		}
		c.chunkLen = 0
	}
	if c.chunkLen == 0 {
		var chunkLenStr string
		for {
			n, err = c.doRead(b[:1])
			if err != nil {
				return
			}
			if n == 0 {
				err = fmt.Errorf("short read")
				return
			}
			c := b[0]
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
				chunkLenStr += string(c)
				continue
			}
			if c == '\r' {
				continue
			}
			if c == '\n' {
				break
			}
			err = fmt.Errorf("unexcepted length character %v", c)
			return
		}
		if len(chunkLenStr) == 0 {
			err = fmt.Errorf("incorrect chunked data")
			return
		}
		var i int64
		i, err = strconv.ParseInt(chunkLenStr, 16, 0)
		if err != nil {
			return
		}
		c.chunkLen = int(i) + 2
	}
	if c.chunkLen == 2 {
		buf := make([]byte, 2)
		_, err = io.ReadFull(&(c.RemainConn), buf)
		if err == nil {
			n = 0
			c.eos = true
			err = fmt.Errorf("read from closed obfsconn")
		}
		return
	}
	var buf []byte
	if c.chunkLen > len(b) {
		buf = b
	} else {
		buf = b[:c.chunkLen]
	}
	n, err = c.doRead(buf)
	if err != nil {
		return
	}
	c.chunkLen -= n
	if c.chunkLen < 2 {
		n -= 2 - c.chunkLen
	}
	return
}

func (c *ObfsConn) Read(b []byte) (n int, err error) {
	c.rlock.Lock()
	defer c.rlock.Unlock()
	if c.destroy {
		err = fmt.Errorf("read from closed connection")
		return
	}
	n, err = c.readInLock(b)
	return
}

func NewObfsConn(conn Conn) *ObfsConn {
	return &ObfsConn{RemainConn: RemainConn{Conn: conn}}
}

type RemainConn struct {
	Conn
	remain  []byte
	wremain []byte
}

func DecayRemainConn(conn Conn) Conn {
	rconn, ok := conn.(*RemainConn)
	if ok && len(rconn.remain) == 0 && len(rconn.wremain) == 0 {
		return rconn.Conn
	}
	return conn
}

func (c *RemainConn) Read(b []byte) (n int, err error) {
	if len(c.remain) == 0 {
		return c.Conn.Read(b)
	}
	n = copy(b, c.remain)
	if n == len(c.remain) {
		c.remain = nil
	} else {
		c.remain = c.remain[n:]
	}
	return
}

func (c *RemainConn) Write(b []byte) (n int, err error) {
	if len(c.wremain) != 0 {
		_, err = c.Conn.WriteBuffers([][]byte{c.wremain, b})
		if err != nil {
			return
		}
		c.wremain = nil
		n = len(b)
		return
	}
	return c.Conn.Write(b)
}

func (c *RemainConn) WriteBuffers(b [][]byte) (n int, err error) {
	if len(c.wremain) != 0 {
		bufs := make([][]byte, 0, len(b)+1)
		bufs = append(bufs, c.wremain)
		bufs = append(bufs, b...)
		_, err = c.Conn.WriteBuffers(bufs)
		if err != nil {
			return
		}
		c.wremain = nil
		for _, v := range b {
			n += len(v)
		}
		return
	}
	return c.Conn.WriteBuffers(b)
}

type SimpleTLSConn struct {
	Conn
	sessionID []byte
	frameLen  int
	responsed bool
	wlock     sync.Mutex
}

func (conn *SimpleTLSConn) Read(b []byte) (n int, err error) {
	for conn.frameLen == 0 {
		frameBuf := make([]byte, 5)
		_, err = io.ReadFull(conn.Conn, frameBuf)
		if err != nil {
			return
		}
		conn.frameLen = int(binary.BigEndian.Uint16(frameBuf[3:]))
	}
	if len(b) > conn.frameLen {
		b = b[:conn.frameLen]
	}
	n, err = conn.Conn.Read(b)
	if n > 0 {
		conn.frameLen -= n
	}
	return
}

func (conn *SimpleTLSConn) writeBuffersInLock(bufs [][]byte) (n int, err error) {
	if len(bufs) == 0 {
		return
	}
	var wbufs [][]byte
	if !conn.responsed {
		tlsBuf := utils.GetBuf(len(bufs[0]) + 512)
		defer utils.PutBuf(tlsBuf)
		n += len(bufs[0])
		tlsLen := utils.GenTLSServerHello(tlsBuf, len(bufs[0]), conn.sessionID)
		wbufs = append(wbufs, tlsBuf[:tlsLen])
		wbufs = append(wbufs, bufs[0])
		bufs = bufs[1:]
		conn.responsed = true
	}
	if len(bufs) != 0 {
		frameHeaders := utils.GetBuf(len(bufs)*5)
		defer utils.PutBuf(frameHeaders)
		for it, buf := range bufs {
			frameBuf := frameHeaders[it*5:(it+1)*5]
			copy(frameBuf, []byte{0x17, 0x03, 0x03})
			binary.BigEndian.PutUint16(frameBuf[3:], uint16(len(buf)))
			wbufs = append(wbufs, frameBuf)
			wbufs = append(wbufs, buf)
			n += len(buf)
		}
	}
	_, err = conn.Conn.WriteBuffers(wbufs)
	if err != nil {
		n = 0
	}
	return
}

func (conn *SimpleTLSConn) Write(b []byte) (n int, err error) {
	conn.wlock.Lock()
	defer conn.wlock.Unlock()
	return conn.writeBuffersInLock([][]byte{b})
}

func (conn *SimpleTLSConn) WriteBuffers(b [][]byte) (n int, err error) {
	conn.wlock.Lock()
	defer conn.wlock.Unlock()
	return conn.writeBuffersInLock(b)
	return
}

func DialObfs(target string, c *Config) (conn Conn, err error) {
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
		}
	}()
	if c.pool != nil {
		conn, err = c.pool.GetNonblock()
	}
	if err != nil || c.pool == nil {
		var tconn *TCPConn
		tconn, err = DialTCP(target, c)
		if tconn != nil {
			conn = tconn
		}
	}
	if err != nil {
		return
	}
	var host string
	if len(c.ObfsHost) == 0 {
		host = defaultObfsHost
	} else if len(c.ObfsHost) == 1 {
		host = c.ObfsHost[0]
	} else {
		host = c.ObfsHost[rand.Intn(len(c.ObfsHost))]
	}
	req := buildHTTPRequest(fmt.Sprintf("Host: %s\r\nX-Online-Host: %s\r\n", host, host))
	obfsconn, ok := conn.(*ObfsConn)
	if !ok {
		obfsconn = NewObfsConn(conn)
		obfsconn.pool = c.pool
	}
	obfsconn.wremain = []byte(req)
	obfsconn.resp = true
	conn = obfsconn
	return
}

func obfsAcceptHandler(conn Conn, lis *listener) (c Conn) {
	defer func() {
		if conn != nil && c == nil {
			conn.Close()
		}
	}()
	buf := utils.GetBuf(buffersize)
	defer utils.PutBuf(buf)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	var remain, wremain []byte
	remain = DupBuffer(buf[:n])
	if n > 4 && string(buf[:4]) != "POST" {
		if string(buf[:4]) == "GET " {
			for {
				parser := utils.NewHTTPHeaderParser(utils.GetBuf(buffersize))
				defer utils.PutBuf(parser.GetBuf())
				ok, err := parser.Read(buf[:n])
				if err != nil || ok == false {
					break
				}
				uv, ok := parser.Load([]byte("Upgrade"))
				if ok == false || len(uv) == 0 || !bytes.Equal(uv[0], []byte("websocket")) {
					break
				}
				cv, ok := parser.Load([]byte("Connection"))
				if ok == false || len(cv) == 0 || !bytes.Equal(cv[0], []byte("Upgrade")) {
					break
				}
				remain = DupBuffer(buf[parser.HeaderLen():n])
				wremain = []byte(buildSimpleObfsResponse())
				break
			}
		} else if buf[0] == 0x16 && n > 0x20 {
			tlsVer := binary.BigEndian.Uint16(buf[1:3])
			tlsLen := int(binary.BigEndian.Uint16(buf[3:5])) + 5
			if tlsVer > tls.VersionTLS12 || tlsVer < tls.VersionSSL30 {
				goto OUT
			}
			if tlsLen > n {
				if tlsLen > 16389 {
					goto OUT
				}
				newBuf := utils.GetBuf(tlsLen)
				defer utils.PutBuf(newBuf)
				_, err = io.ReadFull(conn, newBuf[n:tlsLen])
				if err != nil {
					return
				}
				copy(newBuf, buf[:n])
				buf = newBuf
				n = tlsLen
			}
			ok, nh, cliMsg := utils.ParseTLSClientHelloMsg(buf[:n]);
			if ok && cliMsg != nil {
				if len(buf[nh:n]) > 0 {
					conn = &RemainConn{Conn: conn, remain: DupBuffer(buf[nh:n])}
				}
				conn = &SimpleTLSConn{Conn: conn, sessionID: DupBuffer(cliMsg.SessionId)}
				if len(cliMsg.SessionTicket) > 0 {
					conn = &RemainConn{Conn: conn, remain: DupBuffer(cliMsg.SessionTicket)}
				}
				c = conn
				return
			}
			//log.Println(buf[:n], n, ok, cliMsg)
		}
		OUT:
		c = &RemainConn{Conn: conn, remain: remain, wremain: wremain}
		return
	}
	resp := buildHTTPResponse("")
	obfsconn := NewObfsConn(conn)
	obfsconn.remain = remain
	obfsconn.wremain = []byte(resp)
	obfsconn.req = true
	obfsconn.pool = lis.c.pool
	c = obfsconn
	return
}

package v2rayxhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	C "github.com/sagernet/sing-box/constant"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
)

var packetUpPostTimeout = C.TCPTimeout

func (c *Client) transportContext() context.Context {
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

func (c *Client) applyGRPCHeader(request *http.Request) {
	if c.noGRPCHeader || request.Body == nil {
		return
	}
	request.Header.Set("Content-Type", "application/grpc")
}

func (c *Client) dialStreamOne(ctx context.Context, sessionID string, xmuxClient *xmuxClient) (net.Conn, error) {
	_ = sessionID
	pipeReader, pipeWriter := io.Pipe()
	connCtx, connCancel := context.WithCancel(c.transportContext())
	request, err := c.newRequest(connCtx, c.meta.uplinkHTTPMethod, "", "", pipeReader)
	if err != nil {
		connCancel()
		return nil, err
	}
	c.applyGRPCHeader(request)

	conn := newStreamConn(pipeReader, pipeWriter, c.serverAddr, newXmuxRelease(xmuxClient))
	conn.cancel = connCancel
	stopGuard := watchDialContext(ctx, conn.created, func(err error) {
		pipeReader.CloseWithError(err)
		connCancel()
	})
	go func() {
		defer stopGuard()
		response, err := xmuxClient.roundTrip(request)
		if err != nil {
			conn.fail(err)
			return
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			conn.fail(E.New("v2ray-xhttp: unexpected status: ", response.Status))
			return
		}
		conn.setupReader(response.Body, nil)
	}()
	return conn, nil
}

func watchDialContext(ctx context.Context, done <-chan struct{}, onCancel func(error)) func() {
	if ctx.Done() == nil {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-done:
				return
			case <-stop:
				return
			default:
				onCancel(ctx.Err())
			}
		case <-done:
		case <-stop:
		}
	}()
	return func() { close(stop) }
}

func (c *Client) dialStreamUp(ctx context.Context, sessionID string, xmuxClient *xmuxClient) (net.Conn, error) {
	connCtx, connCancel := context.WithCancel(c.transportContext())
	downReq, err := c.newRequest(connCtx, http.MethodGet, sessionID, "", nil)
	if err != nil {
		connCancel()
		return nil, err
	}

	pipeReader, pipeWriter := io.Pipe()
	upReq, err := c.newRequest(connCtx, c.meta.uplinkHTTPMethod, sessionID, "", pipeReader)
	if err != nil {
		connCancel()
		return nil, err
	}
	c.applyGRPCHeader(upReq)
	conn := newSplitConn(pipeReader, pipeWriter, c.serverAddr, newXmuxRelease(xmuxClient))
	conn.cancel = connCancel
	stopGuard := watchDialContext(ctx, conn.created, func(err error) {
		pipeReader.CloseWithError(err)
		connCancel()
	})
	go func() {
		defer stopGuard()
		downResp, err := xmuxClient.roundTrip(downReq)
		if err != nil {
			conn.fail(E.Cause(err, "open download"))
			return
		}
		if downResp.StatusCode != http.StatusOK {
			downResp.Body.Close()
			conn.fail(E.New("v2ray-xhttp: unexpected download status: ", downResp.Status))
			return
		}
		conn.setupReader(downResp.Body, nil)
	}()
	go func() {
		upResp, err := xmuxClient.roundTrip(upReq)
		if err != nil {
			conn.uploadFailed(err)
			return
		}
		if upResp.StatusCode != http.StatusOK {
			conn.uploadFailed(E.New("v2ray-xhttp: unexpected upload status: ", upResp.Status))
			upResp.Body.Close()
			return
		}
		drainAndClose(upResp.Body)
	}()
	return conn, nil
}

func (c *Client) dialPacketUp(ctx context.Context, sessionID string, xmuxClient *xmuxClient) (net.Conn, error) {
	_ = ctx
	connCtx, connCancel := context.WithCancel(c.transportContext())
	downReq, err := c.newRequest(connCtx, http.MethodGet, sessionID, "", nil)
	if err != nil {
		connCancel()
		return nil, err
	}
	conn := newPacketConn(connCtx, c, sessionID, c.serverAddr, newXmuxRelease(xmuxClient))
	conn.cancel = connCancel
	go func() {
		downResp, err := xmuxClient.roundTrip(downReq)
		if err != nil {
			conn.fail(E.Cause(err, "open download"))
			return
		}
		if downResp.StatusCode != http.StatusOK {
			downResp.Body.Close()
			conn.fail(E.New("v2ray-xhttp: unexpected download status: ", downResp.Status))
			return
		}
		conn.setupReader(downResp.Body, nil)
	}()
	return conn, nil
}

type writeDeadline struct {
	access sync.Mutex
	reader *io.PipeReader
	timer  *time.Timer
}

func (d *writeDeadline) set(t time.Time) error {
	d.access.Lock()
	defer d.access.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if t.IsZero() || d.reader == nil {
		return nil
	}
	if delay := time.Until(t); delay <= 0 {
		d.reader.CloseWithError(os.ErrDeadlineExceeded)
	} else {
		d.timer = time.AfterFunc(delay, func() {
			d.reader.CloseWithError(os.ErrDeadlineExceeded)
		})
	}
	return nil
}

func (d *writeDeadline) stop() {
	d.access.Lock()
	defer d.access.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

type readDeadline struct {
	access   sync.Mutex
	dead     chan struct{}
	expired  bool
	timer    *time.Timer
	onExpire func()
}

func newReadDeadline(onExpire func()) *readDeadline {
	return &readDeadline{dead: make(chan struct{}), onExpire: onExpire}
}

func (d *readDeadline) set(t time.Time) error {
	d.access.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if t.IsZero() || d.expired {
		d.access.Unlock()
		return nil
	}
	var fire bool
	if delay := time.Until(t); delay <= 0 {
		fire = d.expireLocked()
	} else {
		d.timer = time.AfterFunc(delay, d.expire)
	}
	d.access.Unlock()
	if fire {
		d.runOnExpire()
	}
	return nil
}

func (d *readDeadline) expire() {
	d.access.Lock()
	fire := d.expireLocked()
	d.access.Unlock()
	if fire {
		d.runOnExpire()
	}
}

func (d *readDeadline) expireLocked() bool {
	if d.expired {
		return false
	}
	d.expired = true
	close(d.dead)
	return d.onExpire != nil
}

func (d *readDeadline) runOnExpire() {
	if d.onExpire != nil {
		d.onExpire()
	}
}

func (d *readDeadline) stop() {
	d.access.Lock()
	defer d.access.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

type connBreaker struct {
	xmux        *xmuxRelease
	localClosed atomic.Bool
	readOK      atomic.Bool
}

func (b *connBreaker) noteRead(err error) {
	if err == nil {
		if !b.readOK.Swap(true) {
			b.xmux.noteSuccess()
		}
		return
	}
	if err != io.EOF && !b.localClosed.Load() {
		b.xmux.noteFailure()
	}
}

type streamConn struct {
	xmux          *xmuxRelease
	breaker       connBreaker
	writer        *io.PipeWriter
	reader        io.ReadCloser
	created       chan struct{}
	readerErr     error
	serverAddr    M.Socksaddr
	closeOnce     sync.Once
	writeDeadline writeDeadline
	readDeadline  *readDeadline
	cancel        context.CancelFunc
}

func newStreamConn(reader *io.PipeReader, writer *io.PipeWriter, serverAddr M.Socksaddr, xmux *xmuxRelease) *streamConn {
	conn := &streamConn{
		xmux:       xmux,
		breaker:    connBreaker{xmux: xmux},
		writer:     writer,
		created:    make(chan struct{}),
		serverAddr: serverAddr,
	}
	conn.writeDeadline.reader = reader
	conn.readDeadline = newReadDeadline(func() {
		conn.breaker.localClosed.Store(true)
		select {
		case <-conn.created:
			if conn.reader != nil {
				conn.reader.Close()
			}
		default:
		}
	})
	return conn
}

func (c *streamConn) setupReader(reader io.ReadCloser, err error) {
	c.reader = reader
	c.readerErr = err
	close(c.created)
}

func (c *streamConn) fail(err error) {
	c.setupReader(nil, err)
	c.writeDeadline.reader.CloseWithError(err)
	if c.cancel != nil {
		c.cancel()
	}
	c.xmux.release()
}

func (c *streamConn) Read(b []byte) (int, error) {
	select {
	case <-c.created:
	case <-c.readDeadline.dead:
		return 0, os.ErrDeadlineExceeded
	}
	if c.readerErr != nil {
		return 0, c.readerErr
	}
	n, err := c.reader.Read(b)
	c.breaker.noteRead(err)
	return n, err
}

func (c *streamConn) Write(b []byte) (int, error) {
	return c.writer.Write(b)
}

func (c *streamConn) Close() error {
	c.closeOnce.Do(func() {
		c.breaker.localClosed.Store(true)
		c.writeDeadline.stop()
		c.readDeadline.stop()
		c.writer.Close()
		select {
		case <-c.created:
			if c.reader != nil {
				c.reader.Close()
			}
		default:
		}
		if c.cancel != nil {
			c.cancel()
		}
		c.xmux.release()
	})
	return nil
}

func (c *streamConn) LocalAddr() net.Addr  { return M.Socksaddr{} }
func (c *streamConn) RemoteAddr() net.Addr { return c.serverAddr }

func (c *streamConn) SetDeadline(t time.Time) error {
	if err := c.readDeadline.set(t); err != nil {
		return err
	}
	return c.writeDeadline.set(t)
}

func (c *streamConn) SetReadDeadline(t time.Time) error  { return c.readDeadline.set(t) }
func (c *streamConn) SetWriteDeadline(t time.Time) error { return c.writeDeadline.set(t) }

func (c *streamConn) NeedAdditionalReadDeadline() bool { return true }

type splitConn struct {
	stateMu       sync.Mutex
	ready         bool
	terminalErr   error
	xmux          *xmuxRelease
	breaker       connBreaker
	reader        io.ReadCloser
	created       chan struct{}
	readerErr     error
	writer        *io.PipeWriter
	serverAddr    M.Socksaddr
	closeOnce     sync.Once
	writeDeadline writeDeadline
	readDeadline  *readDeadline
	cancel        context.CancelFunc
}

func newSplitConn(uploadReader *io.PipeReader, writer *io.PipeWriter, serverAddr M.Socksaddr, xmux *xmuxRelease) *splitConn {
	conn := &splitConn{
		xmux:       xmux,
		breaker:    connBreaker{xmux: xmux},
		created:    make(chan struct{}),
		writer:     writer,
		serverAddr: serverAddr,
	}
	conn.writeDeadline.reader = uploadReader
	conn.readDeadline = newReadDeadline(func() {
		conn.breaker.localClosed.Store(true)
		conn.fail(os.ErrDeadlineExceeded)
	})
	return conn
}

func (c *splitConn) setupReader(reader io.ReadCloser, err error) {
	c.stateMu.Lock()
	if c.ready || c.terminalErr != nil {
		c.stateMu.Unlock()
		if reader != nil {
			reader.Close()
		}
		return
	}
	c.reader = reader
	c.readerErr = err
	c.ready = true
	close(c.created)
	c.stateMu.Unlock()
}

func (c *splitConn) fail(err error) {
	c.stateMu.Lock()
	if c.terminalErr != nil {
		c.stateMu.Unlock()
		return
	}
	c.terminalErr = err
	reader := c.reader
	if !c.ready {
		c.ready = true
		close(c.created)
	}
	c.stateMu.Unlock()
	c.writeDeadline.stop()
	c.readDeadline.stop()
	c.writeDeadline.reader.CloseWithError(err)
	if c.cancel != nil {
		c.cancel()
	}
	if reader != nil {
		reader.Close()
	}
	c.xmux.release()
}

func (c *splitConn) uploadFailed(err error) {
	c.fail(err)
}

func (c *splitConn) Read(b []byte) (int, error) {
	select {
	case <-c.created:
	case <-c.readDeadline.dead:
		c.stateMu.Lock()
		err := c.terminalErr
		c.stateMu.Unlock()
		if err != nil {
			return 0, err
		}
		return 0, os.ErrDeadlineExceeded
	}
	c.stateMu.Lock()
	reader, err := c.reader, c.readerErr
	if c.terminalErr != nil {
		err = c.terminalErr
	}
	c.stateMu.Unlock()
	if err != nil {
		return 0, err
	}
	n, err := reader.Read(b)
	c.stateMu.Lock()
	if c.terminalErr != nil {
		err = c.terminalErr
	}
	c.stateMu.Unlock()
	c.breaker.noteRead(err)
	return n, err
}
func (c *splitConn) Write(b []byte) (int, error) { return c.writer.Write(b) }

func (c *splitConn) Close() error {
	c.closeOnce.Do(func() {
		c.breaker.localClosed.Store(true)
		c.writeDeadline.stop()
		c.readDeadline.stop()
		c.writer.Close()
		c.fail(net.ErrClosed)
	})
	return nil
}

func (c *splitConn) LocalAddr() net.Addr  { return M.Socksaddr{} }
func (c *splitConn) RemoteAddr() net.Addr { return c.serverAddr }

func (c *splitConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *splitConn) SetReadDeadline(t time.Time) error {
	err := c.readDeadline.set(t)
	c.stateMu.Lock()
	terminal := c.terminalErr != nil
	c.stateMu.Unlock()
	if terminal {
		c.readDeadline.stop()
	}
	return err
}
func (c *splitConn) SetWriteDeadline(t time.Time) error {
	err := c.writeDeadline.set(t)
	c.stateMu.Lock()
	terminal := c.terminalErr != nil
	c.stateMu.Unlock()
	if terminal {
		c.writeDeadline.stop()
	}
	return err
}

func (c *splitConn) NeedAdditionalReadDeadline() bool { return true }

type packetConn struct {
	ctx          context.Context
	client       *Client
	xmux         *xmuxRelease
	breaker      connBreaker
	sessionID    string
	reader       io.ReadCloser
	created      chan struct{}
	readerErr    error
	serverAddr   M.Socksaddr
	access       sync.Mutex
	seq          uint64
	lastPost     time.Time
	closed       bool
	closeOnce    sync.Once
	readDeadline *readDeadline
	cancel       context.CancelFunc
}

func newPacketConn(ctx context.Context, client *Client, sessionID string, serverAddr M.Socksaddr, xmux *xmuxRelease) *packetConn {
	conn := &packetConn{
		ctx:        ctx,
		client:     client,
		xmux:       xmux,
		breaker:    connBreaker{xmux: xmux},
		sessionID:  sessionID,
		created:    make(chan struct{}),
		serverAddr: serverAddr,
	}
	conn.readDeadline = newReadDeadline(func() {
		conn.breaker.localClosed.Store(true)
		select {
		case <-conn.created:
			if conn.reader != nil {
				conn.reader.Close()
			}
		default:
		}
	})
	return conn
}

func (c *packetConn) setupReader(reader io.ReadCloser, err error) {
	c.reader = reader
	c.readerErr = err
	close(c.created)
}

func (c *packetConn) fail(err error) {
	c.setupReader(nil, err)
	if c.cancel != nil {
		c.cancel()
	}
	c.xmux.release()
}

func (c *packetConn) Read(b []byte) (int, error) {
	select {
	case <-c.created:
	case <-c.readDeadline.dead:
		return 0, os.ErrDeadlineExceeded
	}
	if c.readerErr != nil {
		return 0, c.readerErr
	}
	n, err := c.reader.Read(b)
	c.breaker.noteRead(err)
	return n, err
}

func (c *packetConn) Write(b []byte) (int, error) {
	maxEach := c.client.meta.scMaxEachPostBytes.rand()
	if maxEach <= 0 {
		maxEach = len(b)
	}
	written := 0
	for written < len(b) {
		end := written + maxEach
		if end > len(b) {
			end = len(b)
		}
		if err := c.sendPacket(b[written:end]); err != nil {
			return written, err
		}
		written = end
	}
	return written, nil
}

func (c *packetConn) sendPacket(b []byte) error {
	c.access.Lock()
	if c.closed {
		c.access.Unlock()
		return net.ErrClosed
	}
	seq := c.seq
	c.seq++
	wait := c.nextPostDelay()
	c.access.Unlock()

	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return c.ctx.Err()
		case <-timer.C:
		}
	}

	payload := make([]byte, len(b))
	copy(payload, b)
	postCtx, postCancel := context.WithTimeout(c.ctx, packetUpPostTimeout)
	defer postCancel()
	request, err := c.client.newRequest(postCtx, c.client.meta.uplinkHTTPMethod, c.sessionID, strconv.FormatUint(seq, 10), nil)
	if err != nil {
		return err
	}
	c.client.applyUplinkData(request, payload)

	response, err := c.xmux.roundTrip(request)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return E.New("v2ray-xhttp: unexpected upload status: ", response.Status)
	}
	drainAndClose(response.Body)
	c.xmux.noteSuccess()
	return nil
}

func (c *packetConn) nextPostDelay() time.Duration {
	interval := time.Duration(c.client.meta.scMinPostsIntervalMs.rand()) * time.Millisecond
	now := timeNow()
	if c.lastPost.IsZero() || interval <= 0 {
		c.lastPost = now
		return 0
	}
	earliest := c.lastPost.Add(interval)
	if earliest.After(now) {
		c.lastPost = earliest
		return earliest.Sub(now)
	}
	c.lastPost = now
	return 0
}

func (c *packetConn) Close() error {
	c.access.Lock()
	c.closed = true
	c.access.Unlock()
	var err error
	c.closeOnce.Do(func() {
		c.breaker.localClosed.Store(true)
		c.readDeadline.stop()
		select {
		case <-c.created:
			if c.reader != nil {
				err = c.reader.Close()
			}
		default:
		}
		if c.cancel != nil {
			c.cancel()
		}
		c.xmux.release()
	})
	return err
}

func (c *packetConn) LocalAddr() net.Addr  { return M.Socksaddr{} }
func (c *packetConn) RemoteAddr() net.Addr { return c.serverAddr }

func (c *packetConn) SetDeadline(t time.Time) error      { return c.readDeadline.set(t) }
func (c *packetConn) SetReadDeadline(t time.Time) error  { return c.readDeadline.set(t) }
func (c *packetConn) SetWriteDeadline(t time.Time) error { return os.ErrInvalid }

func (c *packetConn) NeedAdditionalReadDeadline() bool { return true }

type byteReader struct {
	data []byte
	off  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

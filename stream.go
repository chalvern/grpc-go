/*
 *
 * Copyright 2014 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package grpc

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/chalvern/grpc-go/balancer"
	"github.com/chalvern/grpc-go/channelz"
	"github.com/chalvern/grpc-go/codes"
	"github.com/chalvern/grpc-go/encoding"
	"github.com/chalvern/grpc-go/metadata"
	"github.com/chalvern/grpc-go/stats"
	"github.com/chalvern/grpc-go/status"
	"github.com/chalvern/grpc-go/transport"
	"golang.org/x/net/context"
	"golang.org/x/net/trace"
)

// StreamHandler defines the handler called by gRPC server to complete the
// execution of a streaming RPC. If a StreamHandler returns an error, it
// should be produced by the status package, or else gRPC will use
// codes.Unknown as the status code and err.Error() as the status message
// of the RPC.
// 定义了gRPC服务端会调用的方法，从而完成流式RPC的执行。如果它返回了error，这个error应该
// 是status包中错误的一种，否则gRPC会使用 codes.Unknown 作为状态码，err.Error()作为
// RPC的状态信息
type StreamHandler func(srv interface{}, stream ServerStream) error

// StreamDesc represents a streaming RPC service's method specification.
//
// 标明一个流式RPC服务的方法定义
type StreamDesc struct {
	StreamName string
	Handler    StreamHandler

	// At least one of these is true.
	// 至少其中一个是true的值。
	ServerStreams bool
	ClientStreams bool
}

// Stream defines the common interface a client or server stream has to satisfy.
//
// All errors returned from Stream are compatible with the status package.
type Stream interface {
	// Context returns the context for this stream.
	Context() context.Context
	// SendMsg blocks until it sends m, the stream is done or the stream
	// breaks.
	// On error, it aborts the stream and returns an RPC status on client
	// side. On server side, it simply returns the error to the caller.
	// SendMsg is called by generated code. Also Users can call SendMsg
	// directly when it is really needed in their use cases.
	// It's safe to have a goroutine calling SendMsg and another goroutine calling
	// recvMsg on the same stream at the same time.
	// But it is not safe to call SendMsg on the same stream in different goroutines.
	SendMsg(m interface{}) error
	// RecvMsg blocks until it receives a message or the stream is
	// done. On client side, it returns io.EOF when the stream is done. On
	// any other error, it aborts the stream and returns an RPC status. On
	// server side, it simply returns the error to the caller.
	// It's safe to have a goroutine calling SendMsg and another goroutine calling
	// recvMsg on the same stream at the same time.
	// But it is not safe to call RecvMsg on the same stream in different goroutines.
	RecvMsg(m interface{}) error
}

// ClientStream defines the interface a client stream has to satisfy.
type ClientStream interface {
	// Header returns the header metadata received from the server if there
	// is any. It blocks if the metadata is not ready to read.
	Header() (metadata.MD, error)
	// Trailer returns the trailer metadata from the server, if there is any.
	// It must only be called after stream.CloseAndRecv has returned, or
	// stream.Recv has returned a non-nil error (including io.EOF).
	Trailer() metadata.MD
	// CloseSend closes the send direction of the stream. It closes the stream
	// when non-nil error is met.
	CloseSend() error
	// Stream.SendMsg() may return a non-nil error when something wrong happens sending
	// the request. The returned error indicates the status of this sending, not the final
	// status of the RPC.
	//
	// Always call Stream.RecvMsg() to drain the stream and get the final
	// status, otherwise there could be leaked resources.
	Stream
}

// NewStream creates a new Stream for the client side. This is typically
// called by generated code. ctx is used for the lifetime of the stream.
//
// To ensure resources are not leaked due to the stream returned, one of the following
// actions must be performed:
//
// 1. call Close on the ClientConn,
// 2. cancel the context provided,
// 3. call RecvMsg until a non-nil error is returned, or
// 4. receive a non-nil, non-io.EOF error from Header or SendMsg.
//
// If none of the above happen, a goroutine and a context will be leaked, and grpc
// will not call the optionally-configured stats handler with a stats.End message.
func (cc *ClientConn) NewStream(ctx context.Context, desc *StreamDesc, method string, opts ...CallOption) (ClientStream, error) {
	// allow interceptor to see all applicable call options, which means those
	// configured as defaults from dial option as well as per-call options
	opts = combine(cc.dopts.callOptions, opts)

	if cc.dopts.streamInt != nil {
		return cc.dopts.streamInt(ctx, desc, cc, method, newClientStream, opts...)
	}
	return newClientStream(ctx, desc, cc, method, opts...)
}

// NewClientStream is a wrapper for ClientConn.NewStream.
//
// DEPRECATED: Use ClientConn.NewStream instead.
func NewClientStream(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (ClientStream, error) {
	return cc.NewStream(ctx, desc, method, opts...)
}

// 创建一个新的客户端流
func newClientStream(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (_ ClientStream, err error) {
	if channelz.IsOn() {
		cc.incrCallsStarted()
		defer func() {
			if err != nil {
				cc.incrCallsFailed()
			}
		}()
	}
	c := defaultCallInfo()
	// 取得方法配置，这个配置可能是后端（服务提供者）给的，也可能是前端默认的
	// 然后据此配置CallInfo
	mc := cc.GetMethodConfig(method)
	if mc.WaitForReady != nil {
		c.failFast = !*mc.WaitForReady //是否是快败模式
	}

	// Possible context leak:
	// The cancel function for the child context we create will only be called
	// when RecvMsg returns a non-nil error, if the ClientConn is closed, or if
	// an error is generated by SendMsg.
	// https://github.com/grpc/grpc-go/issues/1818.
	var cancel context.CancelFunc
	// 根据methodConfig中的超时字段生成上下文
	if mc.Timeout != nil && *mc.Timeout >= 0 {
		ctx, cancel = context.WithTimeout(ctx, *mc.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	// 在发送RPC调用前，把配置参数给作用上。
	for _, o := range opts {
		if err := o.before(c); err != nil {
			return nil, toRPCErr(err)
		}
	}
	// 配置信息最大的可发送值和最大的接收值
	c.maxSendMessageSize = getMaxSize(mc.MaxReqSize, c.maxSendMessageSize, defaultClientMaxSendMessageSize)
	c.maxReceiveMessageSize = getMaxSize(mc.MaxRespSize, c.maxReceiveMessageSize, defaultClientMaxReceiveMessageSize)
	if err := setCallInfoCodec(c); err != nil {
		return nil, err
	}

	// 封装RPC相关的信息
	callHdr := &transport.CallHdr{
		Host:   cc.authority,
		Method: method,
		// If it's not client streaming, we should already have the request to be sent,
		// so we don't flush the header.
		// If it's client streaming, the user may never send a request or send it any
		// time soon, so we ask the transport to flush the header.
		//
		// 如果不是客户端流，这种情况下应该已经有请求发送了，因此不需要在flush请求头；
		// 如果是客户端流，用户或许还没发送过请求，或许马上就要发送，因此让传输层flush请求头。
		Flush:          desc.ClientStreams,
		ContentSubtype: c.contentSubtype,
	}

	// Set our outgoing compression according to the UseCompressor CallOption, if
	// set.  In that case, also find the compressor from the encoding package.
	// Otherwise, use the compressor configured by the WithCompressor DialOption,
	// if set.
	//
	// 如果 UseCompressor CallOption 已经被设置，根据这个来设置出口的压缩方法；此时从编码
	// 包查找合适的压缩器。否则，如果 WithCompressor DialOption 设置了就使用它设置。
	var cp Compressor
	var comp encoding.Compressor
	if ct := c.compressorType; ct != "" {
		// 根据 UserCompressor CallOption 来设置 comp
		callHdr.SendCompress = ct
		if ct != encoding.Identity {
			comp = encoding.GetCompressor(ct)
			if comp == nil {
				return nil, status.Errorf(codes.Internal, "grpc: Compressor is not installed for requested grpc-encoding %q", ct)
			}
		}
	} else if cc.dopts.cp != nil {
		// 根据 DiaOption 来设置 cp
		callHdr.SendCompress = cc.dopts.cp.Type()
		cp = cc.dopts.cp
	}
	// 设置证书
	if c.creds != nil {
		callHdr.Creds = c.creds
	}
	// 设置tracing
	var trInfo traceInfo
	if EnableTracing {
		trInfo.tr = trace.New("grpc.Sent."+methodFamily(method), method)
		trInfo.firstLine.client = true
		if deadline, ok := ctx.Deadline(); ok {
			trInfo.firstLine.deadline = deadline.Sub(time.Now())
		}
		trInfo.tr.LazyLog(&trInfo.firstLine, false)
		ctx = trace.NewContext(ctx, trInfo.tr)
		defer func() {
			if err != nil {
				// Need to call tr.finish() if error is returned.
				// Because tr will not be returned to caller.
				trInfo.tr.LazyPrintf("RPC: [%v]", err)
				trInfo.tr.SetError()
				trInfo.tr.Finish()
			}
		}()
	}
	ctx = newContextWithRPCInfo(ctx, c.failFast)
	// 设置统计相关
	sh := cc.dopts.copts.StatsHandler
	var beginTime time.Time
	if sh != nil {
		ctx = sh.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: method, FailFast: c.failFast})
		beginTime = time.Now()
		begin := &stats.Begin{
			Client:    true,
			BeginTime: beginTime,
			FailFast:  c.failFast,
		}
		sh.HandleRPC(ctx, begin)
		defer func() {
			if err != nil {
				// Only handle end stats if err != nil.
				end := &stats.End{
					Client:    true,
					Error:     err,
					BeginTime: beginTime,
					EndTime:   time.Now(),
				}
				sh.HandleRPC(ctx, end)
			}
		}()
	}

	var (
		t    transport.ClientTransport
		s    *transport.Stream
		done func(balancer.DoneInfo)
	)
	for {
		// Check to make sure the context has expired.  This will prevent us from
		// looping forever if an error occurs for wait-for-ready RPCs where no data
		// is sent on the wire.
		//
		// 确认context已过期。通过这个可以防止一些情况下的无限循环，比如当没有数据在wire中发送，
		// wait-for-ready RPCs调用发生错误时。
		select {
		case <-ctx.Done():
			return nil, toRPCErr(ctx.Err())
		default:
		}

		// 获取客户端传输通道
		t, done, err = cc.getTransport(ctx, c.failFast)
		if err != nil {
			return nil, err
		}

		// 为一个RPC调用创建一个流
		s, err = t.NewStream(ctx, callHdr)
		if err != nil {
			if done != nil {
				done(balancer.DoneInfo{Err: err})
				done = nil
			}
			// In the event of any error from NewStream, we never attempted to write
			// anything to the wire, so we can retry indefinitely for non-fail-fast
			// RPCs.
			// 如果不是failFast模式，就无限尝试
			if !c.failFast {
				continue
			}
			return nil, toRPCErr(err)
		}
		break
	}

	// 搞一个客户端流
	cs := &clientStream{
		opts:   opts,
		c:      c,
		cc:     cc,
		desc:   desc,
		codec:  c.codec,
		cp:     cp,
		comp:   comp,
		cancel: cancel,
		attempt: &csAttempt{
			t:            t,
			s:            s,
			p:            &parser{r: s},
			done:         done,
			dc:           cc.dopts.dc,
			ctx:          ctx,
			trInfo:       trInfo,
			statsHandler: sh,
			beginTime:    beginTime,
		},
	}
	cs.c.stream = cs
	cs.attempt.cs = cs
	// 如果传入的不是一元流描述
	if desc != unaryStreamDesc {
		// Listen on cc and stream contexts to cleanup when the user closes the
		// ClientConn or cancels the stream context.  In all other cases, an error
		// should already be injected into the recv buffer by the transport, which
		// the client will eventually receive, and then we will cancel the stream's
		// context in clientStream.finish.
		//
		// 监听客户端连接（cc）和流上下文（ctx），当用户关闭客户端连接或取消流时清理资源
		// 在其他情况下，接收缓存中应该已经被注入了错误信息，当客户端获取这个错误信息，然后
		// 就可以在clientStream.finish取消流上下文了
		go func() {
			select {
			case <-cc.ctx.Done():
				cs.finish(ErrClientConnClosing)
			case <-ctx.Done():
				cs.finish(toRPCErr(ctx.Err()))
			}
		}()
	}
	return cs, nil
}

// clientStream implements a client side Stream.
// 实现了客户端流
type clientStream struct {
	opts []CallOption
	c    *callInfo
	cc   *ClientConn
	desc *StreamDesc

	codec baseCodec
	cp    Compressor
	comp  encoding.Compressor

	cancel context.CancelFunc // cancels all attempts

	sentLast bool // sent an end stream

	mu       sync.Mutex // guards finished
	finished bool       // TODO: replace with atomic cmpxchg or sync.Once?

	attempt *csAttempt // the active client stream attempt
	// TODO(hedging): hedging will have multiple attempts simultaneously.
}

// csAttempt implements a single transport stream attempt within a
// clientStream.
// 封装一个客户端流的一次传输流的尝试
type csAttempt struct {
	cs   *clientStream
	t    transport.ClientTransport
	s    *transport.Stream
	p    *parser
	done func(balancer.DoneInfo)

	dc        Decompressor
	decomp    encoding.Compressor
	decompSet bool

	ctx context.Context // the application's context, wrapped by stats/tracing

	mu sync.Mutex // guards trInfo.tr
	// trInfo.tr is set when created (if EnableTracing is true),
	// and cleared when the finish method is called.
	trInfo traceInfo

	statsHandler stats.Handler
	beginTime    time.Time
}

func (cs *clientStream) Context() context.Context {
	// TODO(retry): commit the current attempt (the context has peer-aware data).
	return cs.attempt.context()
}

func (cs *clientStream) Header() (metadata.MD, error) {
	m, err := cs.attempt.header()
	if err != nil {
		// TODO(retry): maybe retry on error or commit attempt on success.
		err = toRPCErr(err)
		cs.finish(err)
	}
	return m, err
}

func (cs *clientStream) Trailer() metadata.MD {
	// TODO(retry): on error, maybe retry (trailers-only).
	return cs.attempt.trailer()
}

// 发送rpc调用
func (cs *clientStream) SendMsg(m interface{}) (err error) {
	// TODO(retry): buffer message for replaying if not committed.
	return cs.attempt.sendMsg(m)
}

// 接受rpc返回信息
func (cs *clientStream) RecvMsg(m interface{}) (err error) {
	// TODO(retry): maybe retry on error or commit attempt on success.
	return cs.attempt.recvMsg(m)
}

func (cs *clientStream) CloseSend() error {
	cs.attempt.closeSend()
	return nil
}

// 结束客户端流
func (cs *clientStream) finish(err error) {
	if err == io.EOF {
		// Ending a stream with EOF indicates a success.
		err = nil
	}
	cs.mu.Lock()
	if cs.finished {
		cs.mu.Unlock()
		return
	}
	cs.finished = true
	cs.mu.Unlock()
	if channelz.IsOn() {
		if err != nil {
			cs.cc.incrCallsFailed()
		} else {
			cs.cc.incrCallsSucceeded()
		}
	}
	// TODO(retry): commit current attempt if necessary.
	cs.attempt.finish(err)
	for _, o := range cs.opts {
		o.after(cs.c)
	}
	cs.cancel()
}

func (a *csAttempt) context() context.Context {
	return a.s.Context()
}

func (a *csAttempt) header() (metadata.MD, error) {
	return a.s.Header()
}

func (a *csAttempt) trailer() metadata.MD {
	return a.s.Trailer()
}

// 发送信息，真正干活的函数
func (a *csAttempt) sendMsg(m interface{}) (err error) {
	// TODO Investigate how to signal the stats handling party.
	// generate error stats if err != nil && err != io.EOF?
	cs := a.cs
	defer func() {
		// For non-client-streaming RPCs, we return nil instead of EOF on success
		// because the generated code requires it.  finish is not called; RecvMsg()
		// will call it with the stream's status independently.
		//
		// 对于非客户端流的RPCs调用，在成功情况下返回nil而不是EOF（因为生成的代码需要）
		// 没有调用finish函数；RecvMsg()会独立地调用finish函数
		if err == io.EOF && !cs.desc.ClientStreams {
			err = nil
		}
		if err != nil && err != io.EOF {
			// Call finish on the client stream for errors generated by this SendMsg
			// call, as these indicate problems created by this client.  (Transport
			// errors are converted to an io.EOF error below; the real error will be
			// returned from RecvMsg eventually in that case, or be retried.)
			//
			// 为生成的err在客户端流调用finish函数，从而标明这个客户端有问题（传输层错误会在
			// 下面转换成io.EOF错误；真正的错误会从RecvMsg返回，或者被重试。
			cs.finish(err)
		}
	}()
	// TODO: Check cs.sentLast and error if we already ended the stream.
	if EnableTracing {
		a.mu.Lock()
		if a.trInfo.tr != nil {
			a.trInfo.tr.LazyLog(&payload{sent: true, msg: m}, true)
		}
		a.mu.Unlock()
	}
	data, err := encode(cs.codec, m)
	if err != nil {
		return err
	}
	compData, err := compress(data, cs.cp, cs.comp)
	if err != nil {
		return err
	}
	hdr, payload := msgHeader(data, compData)
	// TODO(dfawley): should we be checking len(data) instead?
	if len(payload) > *cs.c.maxSendMessageSize {
		return status.Errorf(codes.ResourceExhausted, "trying to send message larger than max (%d vs. %d)", len(payload), *cs.c.maxSendMessageSize)
	}

	if !cs.desc.ClientStreams {
		cs.sentLast = true
	}
	err = a.t.Write(a.s, hdr, payload, &transport.Options{Last: !cs.desc.ClientStreams})
	if err == nil {
		if a.statsHandler != nil {
			a.statsHandler.HandleRPC(a.ctx, outPayload(true, m, data, payload, time.Now()))
		}
		if channelz.IsOn() {
			a.t.IncrMsgSent()
		}
		return nil
	}
	return io.EOF
}

// 接收信息，真正干活的函数
func (a *csAttempt) recvMsg(m interface{}) (err error) {
	cs := a.cs
	defer func() {
		if err != nil || !cs.desc.ServerStreams {
			// err != nil or non-server-streaming indicates end of stream.
			cs.finish(err)
		}
	}()
	var inPayload *stats.InPayload
	if a.statsHandler != nil {
		inPayload = &stats.InPayload{
			Client: true,
		}
	}
	if !a.decompSet {
		// Block until we receive headers containing received message encoding.
		if ct := a.s.RecvCompress(); ct != "" && ct != encoding.Identity {
			if a.dc == nil || a.dc.Type() != ct {
				// No configured decompressor, or it does not match the incoming
				// message encoding; attempt to find a registered compressor that does.
				a.dc = nil
				a.decomp = encoding.GetCompressor(ct)
			}
		} else {
			// No compression is used; disable our decompressor.
			a.dc = nil
		}
		// Only initialize this state once per stream.
		a.decompSet = true
	}
	err = recv(a.p, cs.codec, a.s, a.dc, m, *cs.c.maxReceiveMessageSize, inPayload, a.decomp)
	if err != nil {
		if err == io.EOF {
			if statusErr := a.s.Status().Err(); statusErr != nil {
				return statusErr
			}
			return io.EOF // indicates successful end of stream.
		}
		return toRPCErr(err)
	}
	if EnableTracing {
		a.mu.Lock()
		if a.trInfo.tr != nil {
			a.trInfo.tr.LazyLog(&payload{sent: false, msg: m}, true)
		}
		a.mu.Unlock()
	}
	if inPayload != nil {
		a.statsHandler.HandleRPC(a.ctx, inPayload)
	}
	if channelz.IsOn() {
		a.t.IncrMsgRecv()
	}
	if cs.desc.ServerStreams {
		// Subsequent messages should be received by subsequent RecvMsg calls.
		return nil
	}

	// Special handling for non-server-stream rpcs.
	// This recv expects EOF or errors, so we don't collect inPayload.
	// 非服务端流的rpcs调用，要有两次 recv 调用（需要跟进，non-server-stream 是啥？为啥需要两次recv）
	err = recv(a.p, cs.codec, a.s, a.dc, m, *cs.c.maxReceiveMessageSize, nil, a.decomp)
	if err == nil {
		return toRPCErr(errors.New("grpc: client streaming protocol violation: get <nil>, want <EOF>"))
	}
	if err == io.EOF {
		return a.s.Status().Err() // non-server streaming Recv returns nil on success
	}
	return toRPCErr(err)
}

func (a *csAttempt) closeSend() {
	cs := a.cs
	if cs.sentLast {
		return
	}
	cs.sentLast = true
	cs.attempt.t.Write(cs.attempt.s, nil, nil, &transport.Options{Last: true})
	// We ignore errors from Write.  Any error it would return would also be
	// returned by a subsequent RecvMsg call, and the user is supposed to always
	// finish the stream by calling RecvMsg until it returns err != nil.
}

func (a *csAttempt) finish(err error) {
	a.mu.Lock()
	a.t.CloseStream(a.s, err)

	if a.done != nil {
		a.done(balancer.DoneInfo{
			Err:           err,
			BytesSent:     true,
			BytesReceived: a.s.BytesReceived(),
		})
	}
	if a.statsHandler != nil {
		end := &stats.End{
			Client:    true,
			BeginTime: a.beginTime,
			EndTime:   time.Now(),
			Error:     err,
		}
		a.statsHandler.HandleRPC(a.ctx, end)
	}
	if a.trInfo.tr != nil {
		if err == nil {
			a.trInfo.tr.LazyPrintf("RPC: [OK]")
		} else {
			a.trInfo.tr.LazyPrintf("RPC: [%v]", err)
			a.trInfo.tr.SetError()
		}
		a.trInfo.tr.Finish()
		a.trInfo.tr = nil
	}
	a.mu.Unlock()
}

// ServerStream defines the interface a server stream has to satisfy.
type ServerStream interface {
	// SetHeader sets the header metadata. It may be called multiple times.
	// When call multiple times, all the provided metadata will be merged.
	// All the metadata will be sent out when one of the following happens:
	//  - ServerStream.SendHeader() is called;
	//  - The first response is sent out;
	//  - An RPC status is sent out (error or success).
	SetHeader(metadata.MD) error
	// SendHeader sends the header metadata.
	// The provided md and headers set by SetHeader() will be sent.
	// It fails if called multiple times.
	SendHeader(metadata.MD) error
	// SetTrailer sets the trailer metadata which will be sent with the RPC status.
	// When called more than once, all the provided metadata will be merged.
	SetTrailer(metadata.MD)
	Stream
}

// serverStream implements a server side Stream.
type serverStream struct {
	ctx   context.Context
	t     transport.ServerTransport
	s     *transport.Stream
	p     *parser
	codec baseCodec

	cp     Compressor
	dc     Decompressor
	comp   encoding.Compressor
	decomp encoding.Compressor

	maxReceiveMessageSize int
	maxSendMessageSize    int
	trInfo                *traceInfo

	statsHandler stats.Handler

	mu sync.Mutex // protects trInfo.tr after the service handler runs.
}

func (ss *serverStream) Context() context.Context {
	return ss.ctx
}

func (ss *serverStream) SetHeader(md metadata.MD) error {
	if md.Len() == 0 {
		return nil
	}
	return ss.s.SetHeader(md)
}

func (ss *serverStream) SendHeader(md metadata.MD) error {
	return ss.t.WriteHeader(ss.s, md)
}

func (ss *serverStream) SetTrailer(md metadata.MD) {
	if md.Len() == 0 {
		return
	}
	ss.s.SetTrailer(md)
}

func (ss *serverStream) SendMsg(m interface{}) (err error) {
	defer func() {
		if ss.trInfo != nil {
			ss.mu.Lock()
			if ss.trInfo.tr != nil {
				if err == nil {
					ss.trInfo.tr.LazyLog(&payload{sent: true, msg: m}, true)
				} else {
					ss.trInfo.tr.LazyLog(&fmtStringer{"%v", []interface{}{err}}, true)
					ss.trInfo.tr.SetError()
				}
			}
			ss.mu.Unlock()
		}
		if err != nil && err != io.EOF {
			st, _ := status.FromError(toRPCErr(err))
			ss.t.WriteStatus(ss.s, st)
		}
		if channelz.IsOn() && err == nil {
			ss.t.IncrMsgSent()
		}
	}()
	data, err := encode(ss.codec, m)
	if err != nil {
		return err
	}
	compData, err := compress(data, ss.cp, ss.comp)
	if err != nil {
		return err
	}
	hdr, payload := msgHeader(data, compData)
	// TODO(dfawley): should we be checking len(data) instead?
	if len(payload) > ss.maxSendMessageSize {
		return status.Errorf(codes.ResourceExhausted, "trying to send message larger than max (%d vs. %d)", len(payload), ss.maxSendMessageSize)
	}
	if err := ss.t.Write(ss.s, hdr, payload, &transport.Options{Last: false}); err != nil {
		return toRPCErr(err)
	}
	if ss.statsHandler != nil {
		ss.statsHandler.HandleRPC(ss.s.Context(), outPayload(false, m, data, payload, time.Now()))
	}
	return nil
}

func (ss *serverStream) RecvMsg(m interface{}) (err error) {
	defer func() {
		if ss.trInfo != nil {
			ss.mu.Lock()
			if ss.trInfo.tr != nil {
				if err == nil {
					ss.trInfo.tr.LazyLog(&payload{sent: false, msg: m}, true)
				} else if err != io.EOF {
					ss.trInfo.tr.LazyLog(&fmtStringer{"%v", []interface{}{err}}, true)
					ss.trInfo.tr.SetError()
				}
			}
			ss.mu.Unlock()
		}
		if err != nil && err != io.EOF {
			st, _ := status.FromError(toRPCErr(err))
			ss.t.WriteStatus(ss.s, st)
		}
		if channelz.IsOn() && err == nil {
			ss.t.IncrMsgRecv()
		}
	}()
	var inPayload *stats.InPayload
	if ss.statsHandler != nil {
		inPayload = &stats.InPayload{}
	}
	if err := recv(ss.p, ss.codec, ss.s, ss.dc, m, ss.maxReceiveMessageSize, inPayload, ss.decomp); err != nil {
		if err == io.EOF {
			return err
		}
		if err == io.ErrUnexpectedEOF {
			err = status.Errorf(codes.Internal, io.ErrUnexpectedEOF.Error())
		}
		return toRPCErr(err)
	}
	if inPayload != nil {
		ss.statsHandler.HandleRPC(ss.s.Context(), inPayload)
	}
	return nil
}

// MethodFromServerStream returns the method string for the input stream.
// The returned string is in the format of "/service/method".
func MethodFromServerStream(stream ServerStream) (string, bool) {
	return Method(stream.Context())
}

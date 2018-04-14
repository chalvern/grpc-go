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
	"golang.org/x/net/context"
)

// Invoke sends the RPC request on the wire and returns after response is
// received.  This is typically called by generated code.
//
// All errors returned by Invoke are compatible with the status package.
//
// 在线路上发送一个RPC请求，在收到响应后返回。这个函数只被自动生成的代码调用。
//
// 返回的所有错误都与status包相关内容兼容
func (cc *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...CallOption) error {
	// allow interceptor to see all applicable call options, which means those
	// configured as defaults from dial option as well as per-call options
	//
	// 允许拦截器查看所有可用的调用配置，即从dial配置中传入的配置项会和每次调用
	// 传入的配置项一起作用在调用过程中。
	opts = combine(cc.dopts.callOptions, opts)

	if cc.dopts.unaryInt != nil {
		return cc.dopts.unaryInt(ctx, method, args, reply, cc, invoke, opts...)
	}
	return invoke(ctx, method, args, reply, cc, opts...)
}

func combine(o1 []CallOption, o2 []CallOption) []CallOption {
	// we don't use append because o1 could have extra capacity whose
	// elements would be overwritten, which could cause inadvertent
	// sharing (and race connditions) between concurrent calls
	//
	// 这里不是使用append方法，因为此时o1可能有多余的容量，否则多余容量中的元素
	// 就会被复写，从而容易在并发情况下导致一些不愿看到的分享（和竞争条件）。
	if len(o1) == 0 {
		return o2
	} else if len(o2) == 0 {
		return o1
	}
	ret := make([]CallOption, len(o1)+len(o2))
	copy(ret, o1)
	copy(ret[len(o1):], o2)
	return ret
}

// Invoke sends the RPC request on the wire and returns after response is
// received.  This is typically called by generated code.
//
// DEPRECATED: Use ClientConn.Invoke instead.
//
// 在线路上发送一个RPC请求，在收到响应后返回。这个函数只被自动生成的代码调用。
func Invoke(ctx context.Context, method string, args, reply interface{}, cc *ClientConn, opts ...CallOption) error {
	return cc.Invoke(ctx, method, args, reply, opts...)
}

var unaryStreamDesc = &StreamDesc{ServerStreams: false, ClientStreams: false}

// Invoke的实际运行逻辑
func invoke(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error {
	// TODO: implement retries in clientStream and make this simply
	// newClientStream, SendMsg, RecvMsg.
	//
	// TODO：在clientStream中实现重试机制并把newClientStream, SendMsg, RecvMsg进行简化
	firstAttempt := true
	for {
		// 1）创建一个客户端流
		csInt, err := newClientStream(ctx, unaryStreamDesc, cc, method, opts...)
		if err != nil {
			return err
		}
		cs := csInt.(*clientStream)
		// 2）发送rpc调用，req是proto里定义的request结构
		if err := cs.SendMsg(req); err != nil {
			if !cs.c.failFast && cs.attempt.s.Unprocessed() && firstAttempt {
				// TODO: Add a field to header for grpc-transparent-retry-attempts
				firstAttempt = false
				continue
			}
			return err
		}
		// 3）接收rpc返回
		if err := cs.RecvMsg(reply); err != nil {
			if !cs.c.failFast && cs.attempt.s.Unprocessed() && firstAttempt {
				// TODO: Add a field to header for grpc-transparent-retry-attempts
				firstAttempt = false
				continue
			}
			return err
		}
		return nil
	}
}

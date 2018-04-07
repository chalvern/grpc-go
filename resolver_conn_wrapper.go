/*
 *
 * Copyright 2017 gRPC authors.
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
	"fmt"
	"strings"

	"github.com/chalvern/grpc-go/grpclog"
	"github.com/chalvern/grpc-go/resolver"
)

// ccResolverWrapper is a wrapper on top of cc for resolvers.
// It implements resolver.ClientConnection interface.
//
// ccResolverWrapper是对解析器的一层封装，用来给上层的cc（客户端连接）使用
// 它会实现resolver.ClientConnection的接口方法
type ccResolverWrapper struct {
	cc       *ClientConn
	resolver resolver.Resolver
	addrCh   chan []resolver.Address
	scCh     chan string
	done     chan struct{}
}

// split2 returns the values from strings.SplitN(s, sep, 2).
// If sep is not found, it returns ("", s, false) instead.
func split2(s, sep string) (string, string, bool) {
	spl := strings.SplitN(s, sep, 2)
	if len(spl) < 2 {
		return "", "", false
	}
	return spl[0], spl[1], true
}

// parseTarget splits target into a struct containing scheme, authority and
// endpoint.
//
// If target is not a valid scheme://authority/endpoint, it returns {Endpoint:
// target}.
//
// parseTarget把目标地址拆分成一个结构体，包含 协议、认证和终端
// 如果目标地址地址不是 scheme://authority/endpoint 这种样式，
// 则返回 {Endpoint: target}
func parseTarget(target string) (ret resolver.Target) {
	var ok bool
	ret.Scheme, ret.Endpoint, ok = split2(target, "://")
	if !ok {
		return resolver.Target{Endpoint: target}
	}
	ret.Authority, ret.Endpoint, ok = split2(ret.Endpoint, "/")
	if !ok {
		return resolver.Target{Endpoint: target}
	}
	return ret
}

// newCCResolverWrapper parses cc.target for scheme and gets the resolver
// builder for this scheme. It then builds the resolver and starts the
// monitoring goroutine for it.
//
// If withResolverBuilder dial option is set, the specified resolver will be
// used instead.
//
// newCCResolverWrapper解析cc.target获取得到协议名并得到相关协议的解析器构建器。
// 然后构建解析器并启动一个监视器监视它。
//
// 如果withResolverBuilder拨号参数设置了，将会用特定的解析器。
func newCCResolverWrapper(cc *ClientConn) (*ccResolverWrapper, error) {
	rb := cc.dopts.resolverBuilder
	if rb == nil {
		return nil, fmt.Errorf("could not get resolver for scheme: %q", cc.parsedTarget.Scheme)
	}

	ccr := &ccResolverWrapper{
		cc:     cc,
		addrCh: make(chan []resolver.Address, 1),
		scCh:   make(chan string, 1),
		done:   make(chan struct{}),
	}

	var err error
	// 根据resolverBuilder中的Build方法来构建一个解析器
	ccr.resolver, err = rb.Build(cc.parsedTarget, ccr, resolver.BuildOption{})
	if err != nil {
		return nil, err
	}
	return ccr, nil
}

func (ccr *ccResolverWrapper) start() {
	go ccr.watcher()
}

// watcher processes address updates and service config updates sequencially.
// Otherwise, we need to resolve possible races between address and service
// config (e.g. they specify different balancer types).
//
// 顺序地监控地址更新和服务端配置的更新。
// 如果不这样做，我们需要专门处理地址和服务配置之间的竞争（比如，他们指定了不同的均衡类型）
// 【推测】言外之意，这里可能蕴含一层关系：肯定会先收到 ccr.addrCh 的信号量，然后才收到
// ccr.scCh 的信号量？（2018-04-07）
func (ccr *ccResolverWrapper) watcher() {
	for {
		select {
		case <-ccr.done:
			return
		default:
		}

		select {
		// 如果收到地址的信号
		case addrs := <-ccr.addrCh:
			select {
			case <-ccr.done:
				return
			default:
			}
			grpclog.Infof("ccResolverWrapper: sending new addresses to cc: %v", addrs)
			ccr.cc.handleResolvedAddrs(addrs, nil)
		// 如果收到服务端配置的信号
		case sc := <-ccr.scCh:
			select {
			case <-ccr.done:
				return
			default:
			}
			grpclog.Infof("ccResolverWrapper: got new service config: %v", sc)
			ccr.cc.handleServiceConfig(sc)
		case <-ccr.done:
			return
		}
	}
}

func (ccr *ccResolverWrapper) resolveNow(o resolver.ResolveNowOption) {
	ccr.resolver.ResolveNow(o)
}

func (ccr *ccResolverWrapper) close() {
	ccr.resolver.Close()
	close(ccr.done)
}

// NewAddress is called by the resolver implemenetion to send addresses to gRPC.
func (ccr *ccResolverWrapper) NewAddress(addrs []resolver.Address) {
	select {
	case <-ccr.addrCh:
	default:
	}
	ccr.addrCh <- addrs
}

// NewServiceConfig is called by the resolver implemenetion to send service
// configs to gPRC.
func (ccr *ccResolverWrapper) NewServiceConfig(sc string) {
	select {
	case <-ccr.scCh:
	default:
	}
	ccr.scCh <- sc
}

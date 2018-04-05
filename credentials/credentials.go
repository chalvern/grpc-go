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

// Package credentials implements various credentials supported by gRPC library,
// which encapsulate all the state needed by a client to authenticate with a
// server and make various assertions, e.g., about the client's identity, role,
// or whether it is authorized to make a particular call.
//
// credentials 包实现了几种可用于gRPC库的认证方式，这个包封装了客户端认证服务端的所有必需
// 状态，同时提供多种担保（断言），比如：客户的身份、角色，以及其是否被授权发起某个调用。
//
package credentials // import "github.com/chalvern/grpc-go/credentials"

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"golang.org/x/net/context"
)

// alpnProtoStr are the specified application level protocols for gRPC.
// alpnProtoStr 包含了应用层面的协议指示符（这里只有一个h2）
var alpnProtoStr = []string{"h2"}

// PerRPCCredentials defines the common interface for the credentials which need to
// attach security information to every RPC (e.g., oauth2).
//
// PerRPCCredentials 定义了一个认证相关的接口，这些认证（比如oauth2）把所有RPC调用和安全信息
// 绑定到一起
type PerRPCCredentials interface {
	// GetRequestMetadata gets the current request metadata, refreshing
	// tokens if required. This should be called by the transport layer on
	// each request, and the data should be populated in headers or other
	// context. If a status code is returned, it will be used as the status
	// for the RPC. uri is the URI of the entry point for the request.
	// When supported by the underlying implementation, ctx can be used for
	// timeout and cancellation.
	// TODO(zhaoq): Define the set of the qualified keys instead of leaving
	// it as an arbitrary string.
	//
	// GetRequestMetadata 获取当前请求的元数据，如果必要也会刷新token。此函数应该在传输层的每个
	// 请求上调用，同时应该把获取的数据保存在请求头或其他上下文。如果一个状态码已经返回了，则把它当
	// 做RPC的状态码。 uri是请求入口的URI。当底层的实现支持时，ctx可以在超时或取消时使用。
	// TODO（zhaoq）: 用字段的集合替换这里的字符串。
	GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error)

	// RequireTransportSecurity indicates whether the credentials requires
	// transport security.
	// RequireTransportSecurity 表示证书是否需要tls
	RequireTransportSecurity() bool
}

// ProtocolInfo provides information regarding the gRPC wire protocol version,
// security protocol, security protocol version in use, server name, etc.
// 提供了gRPC线协议版本、安全协议及其协议版本、服务名等相关的信息。
type ProtocolInfo struct {
	// ProtocolVersion is the gRPC wire protocol version.
	// 协议版本
	ProtocolVersion string
	// SecurityProtocol is the security protocol in use.
	// 安全协议
	SecurityProtocol string
	// SecurityVersion is the security protocol version.
	// 安全协议版本
	SecurityVersion string
	// ServerName is the user-configured server name.
	// 用户配置的服务名
	ServerName string
}

// AuthInfo defines the common interface for the auth information the users are interested in.
type AuthInfo interface {
	AuthType() string
}

// ErrConnDispatched indicates that rawConn has been dispatched out of gRPC
// and the caller should not close rawConn.
var ErrConnDispatched = errors.New("credentials: rawConn is dispatched out of gRPC")

// TransportCredentials defines the common interface for all the live gRPC wire
// protocols and supported transport security protocols (e.g., TLS, SSL).
// 为所有现有的gRPC线协议和支持的传输安全协议（比如 TLS，SSL）定义了一个通用接口
type TransportCredentials interface {
	// ClientHandshake does the authentication handshake specified by the corresponding
	// authentication protocol on rawConn for clients. It returns the authenticated
	// connection and the corresponding auth information about the connection.
	// Implementations must use the provided context to implement timely cancellation.
	// gRPC will try to reconnect if the error returned is a temporary error
	// (io.EOF, context.DeadlineExceeded or err.Temporary() == true).
	// If the returned error is a wrapper error, implementations should make sure that
	// the error implements Temporary() to have the correct retry behaviors.
	//
	// If the returned net.Conn is closed, it MUST close the net.Conn provided.
	//
	//
	ClientHandshake(context.Context, string, net.Conn) (net.Conn, AuthInfo, error)
	// ServerHandshake does the authentication handshake for servers. It returns
	// the authenticated connection and the corresponding auth information about
	// the connection.
	//
	// If the returned net.Conn is closed, it MUST close the net.Conn provided.
	ServerHandshake(net.Conn) (net.Conn, AuthInfo, error)
	// Info provides the ProtocolInfo of this TransportCredentials.
	// 提供了这个TransportCredentials的ProtocolInfo（感觉像是废话。。）
	Info() ProtocolInfo
	// Clone makes a copy of this TransportCredentials.
	Clone() TransportCredentials
	// OverrideServerName overrides the server name used to verify the hostname on the returned certificates from the server.
	// gRPC internals also use it to override the virtual hosting name if it is set.
	// It must be called before dialing. Currently, this is only used by grpclb.
	OverrideServerName(string) error
}

// TLSInfo contains the auth information for a TLS authenticated connection.
// It implements the AuthInfo interface.
type TLSInfo struct {
	State tls.ConnectionState
}

// AuthType returns the type of TLSInfo as a string.
func (t TLSInfo) AuthType() string {
	return "tls"
}

// tlsCreds is the credentials required for authenticating a connection using TLS.
type tlsCreds struct {
	// TLS configuration
	config *tls.Config
}

func (c tlsCreds) Info() ProtocolInfo {
	return ProtocolInfo{
		SecurityProtocol: "tls",
		SecurityVersion:  "1.2",
		ServerName:       c.config.ServerName,
	}
}

func (c *tlsCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (_ net.Conn, _ AuthInfo, err error) {
	// use local cfg to avoid clobbering ServerName if using multiple endpoints
	cfg := cloneTLSConfig(c.config)
	if cfg.ServerName == "" {
		colonPos := strings.LastIndex(authority, ":")
		if colonPos == -1 {
			colonPos = len(authority)
		}
		cfg.ServerName = authority[:colonPos]
	}
	conn := tls.Client(rawConn, cfg)
	errChannel := make(chan error, 1)
	go func() {
		errChannel <- conn.Handshake()
	}()
	select {
	case err := <-errChannel:
		if err != nil {
			return nil, nil, err
		}
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	return conn, TLSInfo{conn.ConnectionState()}, nil
}

func (c *tlsCreds) ServerHandshake(rawConn net.Conn) (net.Conn, AuthInfo, error) {
	conn := tls.Server(rawConn, c.config)
	if err := conn.Handshake(); err != nil {
		return nil, nil, err
	}
	return conn, TLSInfo{conn.ConnectionState()}, nil
}

func (c *tlsCreds) Clone() TransportCredentials {
	return NewTLS(c.config)
}

func (c *tlsCreds) OverrideServerName(serverNameOverride string) error {
	c.config.ServerName = serverNameOverride
	return nil
}

// NewTLS uses c to construct a TransportCredentials based on TLS.
func NewTLS(c *tls.Config) TransportCredentials {
	tc := &tlsCreds{cloneTLSConfig(c)}
	tc.config.NextProtos = alpnProtoStr
	return tc
}

// NewClientTLSFromCert constructs TLS credentials from the input certificate for client.
// serverNameOverride is for testing only. If set to a non empty string,
// it will override the virtual host name of authority (e.g. :authority header field) in requests.
func NewClientTLSFromCert(cp *x509.CertPool, serverNameOverride string) TransportCredentials {
	return NewTLS(&tls.Config{ServerName: serverNameOverride, RootCAs: cp})
}

// NewClientTLSFromFile constructs TLS credentials from the input certificate file for client.
// serverNameOverride is for testing only. If set to a non empty string,
// it will override the virtual host name of authority (e.g. :authority header field) in requests.
func NewClientTLSFromFile(certFile, serverNameOverride string) (TransportCredentials, error) {
	b, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	return NewTLS(&tls.Config{ServerName: serverNameOverride, RootCAs: cp}), nil
}

// NewServerTLSFromCert constructs TLS credentials from the input certificate for server.
func NewServerTLSFromCert(cert *tls.Certificate) TransportCredentials {
	return NewTLS(&tls.Config{Certificates: []tls.Certificate{*cert}})
}

// NewServerTLSFromFile constructs TLS credentials from the input certificate file and key
// file for server.
func NewServerTLSFromFile(certFile, keyFile string) (TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}}), nil
}

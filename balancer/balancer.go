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

// Package balancer defines APIs for load balancing in gRPC.
// All APIs in this package are experimental.
package balancer

import (
	"errors"
	"net"
	"strings"

	"github.com/chalvern/grpc-go/connectivity"
	"github.com/chalvern/grpc-go/credentials"
	"github.com/chalvern/grpc-go/resolver"
	"golang.org/x/net/context"
)

var (
	// m is a map from name to balancer builder.
	m = make(map[string]Builder)
)

// Register registers the balancer builder to the balancer map.
// b.Name (lowercased) will be used as the name registered with
// this builder.
func Register(b Builder) {
	m[strings.ToLower(b.Name())] = b
}

// Get returns the resolver builder registered with the given name.
// Note that the compare is done in a case-insenstive fashion.
// If no builder is register with the name, nil will be returned.
func Get(name string) Builder {
	if b, ok := m[strings.ToLower(name)]; ok {
		return b
	}
	return nil
}

// SubConn represents a gRPC sub connection.
// Each sub connection contains a list of addresses. gRPC will
// try to connect to them (in sequence), and stop trying the
// remainder once one connection is successful.
//
// The reconnect backoff will be applied on the list, not a single address.
// For example, try_on_all_addresses -> backoff -> try_on_all_addresses.
//
// All SubConns start in IDLE, and will not try to connect. To trigger
// the connecting, Balancers must call Connect.
// When the connection encounters an error, it will reconnect immediately.
// When the connection becomes IDLE, it will not reconnect unless Connect is
// called.
//
// This interface is to be implemented by gRPC. Users should not need a
// brand new implementation of this interface. For the situations like
// testing, the new implementation should embed this interface. This allows
// gRPC to add new methods to this interface.
//
// SubConn 代表gRPC的子连接
// 每个子连接包含一个地址列表， gRPC会顺序地尝试连接到它们，一旦有一个连接成功，就停止尝试
//
// 重试退避算法会应用到真个列表，而不是单个地址，比如：尝试所有地址 -> 退避 -> 再尝试所有地址.
//
// 所有的 SubConns 始于 IDLE 状态，并且不会尝试连接，必须要Balancers调用Connect来触发连接
// 当连接遇到错误，SubConns会立马重新连接；当连接变成IDLE状态，除非Connect被调用否则它也不会
// 重连。
//
// 这个接口会被gRPC实例化，用户不应该为这个接口搞新的实例化。为了测试，可以搞新的实例化，但是应
// 该采取嵌入的方式，从而允许gRPC为这个接口添加新的方法
//
type SubConn interface {
	// UpdateAddresses updates the addresses used in this SubConn.
	// gRPC checks if currently-connected address is still in the new list.
	// If it's in the list, the connection will be kept.
	// If it's not in the list, the connection will gracefully closed, and
	// a new connection will be created.
	//
	// This will trigger a state transition for the SubConn.
	//
	// UpdateAddresses 更新在这个SubConn中使用的地址。
	// gRPC会检查当前连接的地址是否在新列表中，如果在，连接会被保持；否则当前连接会被优雅地关闭，
	// 同时创建一个新的连接
	// 这个方法会触发SubConn的状态转换
	//
	UpdateAddresses([]resolver.Address)
	// Connect starts the connecting for this SubConn.
	Connect()
}

// NewSubConnOptions contains options to create new SubConn.
type NewSubConnOptions struct{}

// ClientConn represents a gRPC ClientConn.
//
// This interface is to be implemented by gRPC. Users should not need a
// brand new implementation of this interface. For the situations like
// testing, the new implementation should embed this interface. This allows
// gRPC to add new methods to this interface.
type ClientConn interface {
	// NewSubConn is called by balancer to create a new SubConn.
	// It doesn't block and wait for the connections to be established.
	// Behaviors of the SubConn can be controlled by options.
	NewSubConn([]resolver.Address, NewSubConnOptions) (SubConn, error)
	// RemoveSubConn removes the SubConn from ClientConn.
	// The SubConn will be shutdown.
	RemoveSubConn(SubConn)

	// UpdateBalancerState is called by balancer to nofity gRPC that some internal
	// state in balancer has changed.
	//
	// gRPC will update the connectivity state of the ClientConn, and will call pick
	// on the new picker to pick new SubConn.
	//
	// UpdateBalancerState 被balancer调用来通知 gRPC， 平衡器的内部状态已发生改变
	// gRPC 会更新 ClientConn 的连接状态，同时会在新的选择器上调用pick方法来选择新的SubConn
	//
	UpdateBalancerState(s connectivity.State, p Picker)

	// ResolveNow is called by balancer to notify gRPC to do a name resolving.
	ResolveNow(resolver.ResolveNowOption)

	// Target returns the dial target for this ClientConn.
	Target() string
}

// BuildOptions contains additional information for Build.
// 包含Build中需要的一些附加信息
type BuildOptions struct {
	// DialCreds is the transport credential the Balancer implementation can
	// use to dial to a remote load balancer server. The Balancer implementations
	// can ignore this if it does not need to talk to another party securely.
	DialCreds credentials.TransportCredentials
	// Dialer is the custom dialer the Balancer implementation can use to dial
	// to a remote load balancer server. The Balancer implementations
	// can ignore this if it doesn't need to talk to remote balancer.
	Dialer func(context.Context, string) (net.Conn, error)
}

// Builder creates a balancer.
type Builder interface {
	// Build creates a new balancer with the ClientConn.
	Build(cc ClientConn, opts BuildOptions) Balancer
	// Name returns the name of balancers built by this builder.
	// It will be used to pick balancers (for example in service config).
	Name() string
}

// PickOptions contains addition information for the Pick operation.
type PickOptions struct{}

// DoneInfo contains additional information for done.
type DoneInfo struct {
	// Err is the rpc error the RPC finished with. It could be nil.
	Err error
	// BytesSent indicates if any bytes have been sent to the server.
	BytesSent bool
	// BytesReceived indicates if any byte has been received from the server.
	BytesReceived bool
}

var (
	// ErrNoSubConnAvailable indicates no SubConn is available for pick().
	// gRPC will block the RPC until a new picker is available via UpdateBalancerState().
	ErrNoSubConnAvailable = errors.New("no SubConn is available")
	// ErrTransientFailure indicates all SubConns are in TransientFailure.
	// WaitForReady RPCs will block, non-WaitForReady RPCs will fail.
	ErrTransientFailure = errors.New("all SubConns are in TransientFailure")
)

// Picker is used by gRPC to pick a SubConn to send an RPC.
// Balancer is expected to generate a new picker from its snapshot everytime its
// internal state has changed.
//
// The pickers used by gRPC can be updated by ClientConn.UpdateBalancerState().
//
// Picker 被gRPC用来选择一个SubConn来发送RPC调用。
// 理想状态下，每当Balancer内部状态改变时都会依据其快照来生成一个新的选择器
//
// 被gRPC使用的选择器可以通过调用  ClientConn.UpdateBalancerState() 方法来更新
type Picker interface {
	// Pick returns the SubConn to be used to send the RPC.
	// The returned SubConn must be one returned by NewSubConn().
	//
	// This functions is expected to return:
	// - a SubConn that is known to be READY;
	// - ErrNoSubConnAvailable if no SubConn is available, but progress is being
	//   made (for example, some SubConn is in CONNECTING mode);
	// - other errors if no active connecting is happening (for example, all SubConn
	//   are in TRANSIENT_FAILURE mode).
	//
	// If a SubConn is returned:
	// - If it is READY, gRPC will send the RPC on it;
	// - If it is not ready, or becomes not ready after it's returned, gRPC will block
	//   until UpdateBalancerState() is called and will call pick on the new picker.
	//
	// If the returned error is not nil:
	// - If the error is ErrNoSubConnAvailable, gRPC will block until UpdateBalancerState()
	// - If the error is ErrTransientFailure:
	//   - If the RPC is wait-for-ready, gRPC will block until UpdateBalancerState()
	//     is called to pick again;
	//   - Otherwise, RPC will fail with unavailable error.
	// - Else (error is other non-nil error):
	//   - The RPC will fail with unavailable error.
	//
	// The returned done() function will be called once the rpc has finished, with the
	// final status of that RPC.
	// done may be nil if balancer doesn't care about the RPC status.
	//
	// Pick 返回用来发送RPC的SubConn，这个SubConn必须是由NewSubConn()返回的
	//
	// 理想状况下，这个方法会返回：
	// 1. 一个确定处于 READY 状态的 SubConn
	// 2. 当没有SubConn可用而过程调用正在进行，则返回ErrNoSubConnAvailable （比如，一些 SubConn
	// 处于 CONNECTING 模式）
	// 3. 如果没有活动的连接可用，返回其他错误（比如所有的 SubConn 都处于 TRANSIENT_FAILURE 模式）
	//
	// 当一个SubConn返回时：
	// 1. 如果它是 READY 的， gRPC会在其上进行发送；
	// 2. 如果它还不可用，或者在其返回后变成不可用，gRPC都会阻塞，直到 UpdateBalancerState() 被调
	// 用，同时会在新的选择器调用pick
	//
	// 返回值中的 done() 函数会在rpc结束时被立马调用
	Pick(ctx context.Context, opts PickOptions) (conn SubConn, done func(DoneInfo), err error)
}

// Balancer takes input from gRPC, manages SubConns, and collects and aggregates
// the connectivity states.
//
// It also generates and updates the Picker used by gRPC to pick SubConns for RPCs.
//
// HandleSubConnectionStateChange, HandleResolvedAddrs and Close are guaranteed
// to be called synchronously from the same goroutine.
// There's no guarantee on picker.Pick, it may be called anytime.
type Balancer interface {
	// HandleSubConnStateChange is called by gRPC when the connectivity state
	// of sc has changed.
	// Balancer is expected to aggregate all the state of SubConn and report
	// that back to gRPC.
	// Balancer should also generate and update Pickers when its internal state has
	// been changed by the new state.
	HandleSubConnStateChange(sc SubConn, state connectivity.State)
	// HandleResolvedAddrs is called by gRPC to send updated resolved addresses to
	// balancers.
	// Balancer can create new SubConn or remove SubConn with the addresses.
	// An empty address slice and a non-nil error will be passed if the resolver returns
	// non-nil error to gRPC.
	HandleResolvedAddrs([]resolver.Address, error)
	// Close closes the balancer. The balancer is not required to call
	// ClientConn.RemoveSubConn for its existing SubConns.
	Close()
}

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

// Package keepalive defines configurable parameters for point-to-point healthcheck.
// keepalive包定义很多配置参数，用于点到点健康检查。
package keepalive

import (
	"time"
)

// ClientParameters is used to set keepalive parameters on the client-side.
// These configure how the client will actively probe to notice when a connection is broken
// and send pings so intermediaries will be aware of the liveness of the connection.
// Make sure these parameters are set in coordination with the keepalive policy on the server,
// as incompatible settings can result in closing of connection.
//
// ClientParameters用于设置客户端的keepalive参数。
// 可以用它来配置客户端的主动探测能力，使得介质（中间层）可以感知到连接失效并从新发起ping请求。
// 需要确认的是，它所代表的参数必须要和服务端的keepalive策略相匹配，否则会使得连接关闭而导致连接失败。
type ClientParameters struct {
	// After a duration of this time if the client doesn't see any activity it pings the server to see if the transport is still alive.
	// 如果这个时间后，客户端没有感知到任何动态，它就会ping服务端来查看是否传输通道仍然是活着的。
	Time time.Duration // The current default value is infinity. 默认指示无限大

	// After having pinged for keepalive check, the client waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	// 客户端允许超时的时间跨度，在这个跨度内，会发起ping来检查连接是否还活着；过了这个跨度，连接就关闭
	Timeout time.Duration // The current default value is 20 seconds. 默认值是20秒

	// If true, client runs keepalive checks even with no active RPCs.
	//如果设置为true，那么即使在没有活动的RPCs调用的情况下，客户端也会运行keepalive检查
	PermitWithoutStream bool // false by default.
}

// ServerParameters is used to set keepalive and max-age parameters on the server-side.
type ServerParameters struct {
	// MaxConnectionIdle is a duration for the amount of time after which an idle connection would be closed by sending a GoAway.
	// Idleness duration is defined since the most recent time the number of outstanding RPCs became zero or the connection establishment.
	MaxConnectionIdle time.Duration // The current default value is infinity.
	// MaxConnectionAge is a duration for the maximum amount of time a connection may exist before it will be closed by sending a GoAway.
	// A random jitter of +/-10% will be added to MaxConnectionAge to spread out connection storms.
	MaxConnectionAge time.Duration // The current default value is infinity.
	// MaxConnectinoAgeGrace is an additive period after MaxConnectionAge after which the connection will be forcibly closed.
	MaxConnectionAgeGrace time.Duration // The current default value is infinity.
	// After a duration of this time if the server doesn't see any activity it pings the client to see if the transport is still alive.
	Time time.Duration // The current default value is 2 hours.
	// After having pinged for keepalive check, the server waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	Timeout time.Duration // The current default value is 20 seconds.
}

// EnforcementPolicy is used to set keepalive enforcement policy on the server-side.
// Server will close connection with a client that violates this policy.
type EnforcementPolicy struct {
	// MinTime is the minimum amount of time a client should wait before sending a keepalive ping.
	MinTime time.Duration // The current default value is 5 minutes.
	// If true, server expects keepalive pings even when there are no active streams(RPCs).
	PermitWithoutStream bool // false by default.
}

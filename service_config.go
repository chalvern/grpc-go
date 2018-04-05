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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chalvern/grpc-go/grpclog"
)

const maxInt = int(^uint(0) >> 1)

// MethodConfig defines the configuration recommended by the service providers for a
// particular method.
// DEPRECATED: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
//
// MethodConfig定义了服务提供者为每个方法推荐使用的配置
//注意：用户不应显式使用这个结构，它应该通过域名解析器来获取。
type MethodConfig struct {
	// WaitForReady indicates whether RPCs sent to this method should wait until
	// the connection is ready by default (!failfast). The value specified via the
	// gRPC client API will override the value set here.
	//
	// WaitForReady标明，默认情况下，发送到这个方法的RPCs调用，在连接就绪前是否应该等待
	// （也就是说不是failfast）。通过gRPC客户端API设置可以覆盖这个值。
	WaitForReady *bool
	// Timeout is the default timeout for RPCs sent to this method. The actual
	// deadline used will be the minimum of the value specified here and the value
	// set by the application via the gRPC client API.  If either one is not set,
	// then the other will be used.  If neither is set, then the RPC has no deadline.
	//
	// Timeout是发送到这个方法的RPCs调用的默认超时时间。实际终止时间不应该大于这个值和应用层
	// 通过gRPC客户端API设置的值，只要任何一个地方设置就ok。如果两个地方都没有设置，那么RPC
	// 调用就不会超时。
	Timeout *time.Duration
	// MaxReqSize is the maximum allowed payload size for an individual request in a
	// stream (client->server) in bytes. The size which is measured is the serialized
	// payload after per-message compression (but before stream compression) in bytes.
	// The actual value used is the minimum of the value specified here and the value set
	// by the application via the gRPC client API. If either one is not set, then the other
	// will be used.  If neither is set, then the built-in default is used.
	//
	// MaxReqSize规定流（客户端到服务端）中的每个请求的最大负载字节。这个尺寸指的是信息被压缩序列化后的值
	// （还没有经过流压缩，这里指出两层压缩，一层是信息层面的压缩，一层是流层的压缩）。
	// 实际最大值不应该大于这个值和应用层通过gRPC客户端API设置的值，只要任何一个地方设置就ok。如果两个地方
	// 都没有设置，那么就会使用内置的默认值。
	MaxReqSize *int
	// MaxRespSize is the maximum allowed payload size for an individual response in a
	// stream (server->client) in bytes.
	// MaxRespSize标明流（服务端到客户端）中每个相应的最大值。
	MaxRespSize *int
}

// ServiceConfig is provided by the service provider and contains parameters for how
// clients that connect to the service should behave.
// DEPRECATED: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
//
// ServiceConfig由服务提供者定义，其参数定义了连接到服务端的客户端的行为
// 注意：用户不应该显式使用这个结构，而是应该通过域名解析器获取。
type ServiceConfig struct {
	// LB is the load balancer the service providers recommends. The balancer specified
	// via grpc.WithBalancer will override this.
	// LB是服务端推荐使用的负载均衡器。通过grpc.WithBalancer传入的值将会覆盖这个值
	LB *string
	// Methods contains a map for the methods in this service.
	// If there is an exact match for a method (i.e. /service/method) in the map, use the corresponding MethodConfig.
	// If there's no exact match, look for the default config for the service (/service/) and use the corresponding MethodConfig if it exists.
	// Otherwise, the method has no MethodConfig to use.
	// Methods是一个字典，存放这个服务提供的方法
	// 如果恰好有一个方法（比如 /service/method）精确匹配，就使用相关的MethodConfig。假如没有，在存在默认配置的情况下会匹配服务默认的配置（/service/）。
	// 否则，就没有MethodConfig使用了。
	Methods map[string]MethodConfig
}

func parseDuration(s *string) (*time.Duration, error) {
	if s == nil {
		return nil, nil
	}
	if !strings.HasSuffix(*s, "s") {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}
	ss := strings.SplitN((*s)[:len(*s)-1], ".", 3)
	if len(ss) > 2 {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}
	// hasDigits is set if either the whole or fractional part of the number is
	// present, since both are optional but one is required.
	hasDigits := false
	var d time.Duration
	if len(ss[0]) > 0 {
		i, err := strconv.ParseInt(ss[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("malformed duration %q: %v", *s, err)
		}
		d = time.Duration(i) * time.Second
		hasDigits = true
	}
	if len(ss) == 2 && len(ss[1]) > 0 {
		if len(ss[1]) > 9 {
			return nil, fmt.Errorf("malformed duration %q", *s)
		}
		f, err := strconv.ParseInt(ss[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("malformed duration %q: %v", *s, err)
		}
		for i := 9; i > len(ss[1]); i-- {
			f *= 10
		}
		d += time.Duration(f)
		hasDigits = true
	}
	if !hasDigits {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}

	return &d, nil
}

type jsonName struct {
	Service *string
	Method  *string
}

func (j jsonName) generatePath() (string, bool) {
	if j.Service == nil {
		return "", false
	}
	res := "/" + *j.Service + "/"
	if j.Method != nil {
		res += *j.Method
	}
	return res, true
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonMC struct {
	Name                    *[]jsonName
	WaitForReady            *bool
	Timeout                 *string
	MaxRequestMessageBytes  *int64
	MaxResponseMessageBytes *int64
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonSC struct {
	LoadBalancingPolicy *string
	MethodConfig        *[]jsonMC
}

func parseServiceConfig(js string) (ServiceConfig, error) {
	var rsc jsonSC
	err := json.Unmarshal([]byte(js), &rsc)
	if err != nil {
		grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
		return ServiceConfig{}, err
	}
	sc := ServiceConfig{
		LB:      rsc.LoadBalancingPolicy,
		Methods: make(map[string]MethodConfig),
	}
	if rsc.MethodConfig == nil {
		return sc, nil
	}

	for _, m := range *rsc.MethodConfig {
		if m.Name == nil {
			continue
		}
		d, err := parseDuration(m.Timeout)
		if err != nil {
			grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
			return ServiceConfig{}, err
		}

		mc := MethodConfig{
			WaitForReady: m.WaitForReady,
			Timeout:      d,
		}
		if m.MaxRequestMessageBytes != nil {
			if *m.MaxRequestMessageBytes > int64(maxInt) {
				mc.MaxReqSize = newInt(maxInt)
			} else {
				mc.MaxReqSize = newInt(int(*m.MaxRequestMessageBytes))
			}
		}
		if m.MaxResponseMessageBytes != nil {
			if *m.MaxResponseMessageBytes > int64(maxInt) {
				mc.MaxRespSize = newInt(maxInt)
			} else {
				mc.MaxRespSize = newInt(int(*m.MaxResponseMessageBytes))
			}
		}
		for _, n := range *m.Name {
			if path, valid := n.generatePath(); valid {
				sc.Methods[path] = mc
			}
		}
	}

	return sc, nil
}

func min(a, b *int) *int {
	if *a < *b {
		return a
	}
	return b
}

func getMaxSize(mcMax, doptMax *int, defaultVal int) *int {
	if mcMax == nil && doptMax == nil {
		return &defaultVal
	}
	if mcMax != nil && doptMax != nil {
		return min(mcMax, doptMax)
	}
	if mcMax != nil {
		return mcMax
	}
	return doptMax
}

func newInt(b int) *int {
	return &b
}

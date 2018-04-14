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

// Package encoding defines the interface for the compressor and codec, and
// functions to register and retrieve compressors and codecs.
//
// This package is EXPERIMENTAL.
//
// 这个包定义了压缩器和解压缩器的接口，以及注册和获取压缩器、解压缩器的方法
package encoding

import (
	"io"
	"strings"
)

// Identity specifies the optional encoding for uncompressed streams.
// It is intended for grpc internal use only.
//
// 定义未压缩数据流的可选编码方式，只用于grpc内部调用。
const Identity = "identity"

// Compressor is used for compressing and decompressing when sending or
// receiving messages.
//
// 压缩器用来在发送数据或接受数据时压缩或解压缩使用。
type Compressor interface {
	// Compress writes the data written to wc to w after compressing it.  If an
	// error occurs while initializing the compressor, that error is returned
	// instead.
	//
	// 把写到wc的数据压缩后写入w，如果在初始化压缩器时报错，就返回那个错误。
	Compress(w io.Writer) (io.WriteCloser, error)
	// Decompress reads data from r, decompresses it, and provides the
	// uncompressed data via the returned io.Reader.  If an error occurs while
	// initializing the decompressor, that error is returned instead.
	//
	// 从r中读取数据，解压缩，通过返回的 io.Reader 提供未压缩的数据。
	// 如果在初始化解压器时报错，返回其相应的错误。
	Decompress(r io.Reader) (io.Reader, error)
	// Name is the name of the compression codec and is used to set the content
	// coding header.  The result must be static; the result cannot change
	// between calls.
	//
	// codec的名称，用来设置内容的编码头。返回结果是静态的，且不能再调用之间变化。
	Name() string
}

var registeredCompressor = make(map[string]Compressor)

// RegisterCompressor registers the compressor with gRPC by its name.  It can
// be activated when sending an RPC via grpc.UseCompressor().  It will be
// automatically accessed when receiving a message based on the content coding
// header.  Servers also use it to send a response with the same encoding as
// the request.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe.  If multiple Compressors are
// registered with the same name, the one registered last will take effect.
//
// RegisterCompressor通过名字来注册gRPC的压缩器。它可以通过grpc.UseCompressor()在
// 发送RPC调用时激活。当获取一个消息时，可以根据内容的编码头自动获取压缩器。服务端会使用
// 请求值中包含的压缩器发送相应。
//
// 注意：这个函数只能在初始化时调用（比如在init()方法内），这个方法不是线程安全的，如果
// 多个压缩器注册为同一个名字，最后的那个生效。
func RegisterCompressor(c Compressor) {
	registeredCompressor[c.Name()] = c
}

// GetCompressor returns Compressor for the given compressor name.
// 根据压缩器名来返回压缩器
func GetCompressor(name string) Compressor {
	return registeredCompressor[name]
}

// Codec defines the interface gRPC uses to encode and decode messages.  Note
// that implementations of this interface must be thread safe; a Codec's
// methods can be called from concurrent goroutines.
type Codec interface {
	// Marshal returns the wire format of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the wire format into v.
	Unmarshal(data []byte, v interface{}) error
	// Name returns the name of the Codec implementation. The returned string
	// will be used as part of content type in transmission.  The result must be
	// static; the result cannot change between calls.
	Name() string
}

var registeredCodecs = make(map[string]Codec, 0)

// RegisterCodec registers the provided Codec for use with all gRPC clients and
// servers.
//
// The Codec will be stored and looked up by result of its Name() method, which
// should match the content-subtype of the encoding handled by the Codec.  This
// is case-insensitive, and is stored and looked up as lowercase.  If the
// result of calling Name() is an empty string, RegisterCodec will panic. See
// Content-Type on
// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md#requests for
// more details.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe.  If multiple Compressors are
// registered with the same name, the one registered last will take effect.
func RegisterCodec(codec Codec) {
	if codec == nil {
		panic("cannot register a nil Codec")
	}
	contentSubtype := strings.ToLower(codec.Name())
	if contentSubtype == "" {
		panic("cannot register Codec with empty string result for String()")
	}
	registeredCodecs[contentSubtype] = codec
}

// GetCodec gets a registered Codec by content-subtype, or nil if no Codec is
// registered for the content-subtype.
//
// The content-subtype is expected to be lowercase.
func GetCodec(contentSubtype string) Codec {
	return registeredCodecs[contentSubtype]
}

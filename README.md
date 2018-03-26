# 说明

这个库是用来学习grpc的库，主要做了两点改动：

* 替换了所有的import，由原来的 google.golang.org/grpc 替换为 github.com/chalvern/grpc-go ，解决依赖路径问题，因此下载本项目后其中的example等可以直接运行。

* 添加一些中文的注释，方便大家参考。

# gRPC-Go

[![Build Status](https://travis-ci.org/grpc/grpc-go.svg)](https://travis-ci.org/grpc/grpc-go) [![GoDoc](https://godoc.org/github.com/chalvern/grpc-go?status.svg)](https://godoc.org/github.com/chalvern/grpc-go) [![GoReportCard](https://goreportcard.com/badge/grpc/grpc-go)](https://goreportcard.com/report/github.com/grpc/grpc-go)

The Go implementation of [gRPC](https://grpc.io/): A high performance, open source, general RPC framework that puts mobile and HTTP/2 first. For more information see the [gRPC Quick Start: Go](https://grpc.io/docs/quickstart/go.html) guide.

Installation
------------

To install this package, you need to install Go and setup your Go workspace on your computer. The simplest way to install the library is to run:

```
$ go get -u github.com/chalvern/grpc-go
```

Prerequisites
-------------

This requires Go 1.6 or later. Go 1.7 will be required soon.

Constraints
-----------
The grpc package should only depend on standard Go packages and a small number of exceptions. If your contribution introduces new dependencies which are NOT in the [list](http://godoc.org/github.com/chalvern/grpc-go?imports), you need a discussion with gRPC-Go authors and consultants.

Documentation
-------------
See [API documentation](https://godoc.org/github.com/chalvern/grpc-go) for package and API descriptions and find examples in the [examples directory](examples/).

Performance
-----------
See the current benchmarks for some of the languages supported in [this dashboard](https://performance-dot-grpc-testing.appspot.com/explore?dashboard=5652536396611584&widget=490377658&container=1286539696).

Status
------
General Availability [Google Cloud Platform Launch Stages](https://cloud.google.com/terms/launch-stages).

FAQ
---

#### Compiling error, undefined: grpc.SupportPackageIsVersion

Please update proto package, gRPC package and rebuild the proto files:
 - `go get -u github.com/golang/protobuf/{proto,protoc-gen-go}`
 - `go get -u github.com/chalvern/grpc-go`
 - `protoc --go_out=plugins=grpc:. *.proto`

// coherence-daemon：v4v6 联动计算守护进程，gRPC over UDS（§2.1）。
package main

import (
	"flag"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
	coherencev1 "github.com/xiaodaoi/ipam/proto/gen/coherence/v1"
)

func main() {
	sock := flag.String("sock", "/run/ipam/coherence.sock", "UDS 监听路径")
	flag.Parse()

	_ = os.Remove(*sock) // 清理陈旧 socket
	lis, err := net.Listen("unix", *sock)
	if err != nil {
		log.Fatalf("listen %s: %v", *sock, err)
	}
	if err := os.Chmod(*sock, 0o660); err != nil {
		log.Printf("chmod socket: %v", err)
	}

	store := coherence.NewMemStore()
	templates := func(id string) (coherence.Template, bool) {
		// PoC 内置默认模板；PG prefix_template 接线在 M1-004/M2
		if id == "t-default" {
			return coherence.Template{ID: id, Prefix: "2406::", Expr: "{v4.hextet4}"}, true
		}
		return coherence.Template{}, false
	}

	gs := grpc.NewServer()
	coherencev1.RegisterCoherenceServer(gs, coherence.NewService(store, templates))
	log.Printf("coherence-daemon listening on %s", *sock)
	if err := gs.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

// coherence-daemon：v4v6 联动计算守护进程，gRPC over UDS（§2.1）。
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
	coherencev1 "github.com/xiaodaoi/ipam/proto/gen/coherence/v1"
)

func main() {
	sock := flag.String("sock", "/run/ipam/coherence.sock", "UDS 监听路径")
	dsn := flag.String("dsn", "", "PG 连接串（ipam 库）；空=纯内存 PoC 模式")
	snapshotPath := flag.String("snapshot", "/run/ipam/bindings.snapshot", "降级快照路径（§2.1）")
	zone := flag.String("zone", "corp.local.", "联动记录权威后缀（§4.4）")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := coherence.NewMemStore()
	var pool *pgxpool.Pool
	if *dsn != "" {
		pool = connectPG(ctx, *dsn, store)
	}

	// 模板装载（M2-013）：PG 模式动态装载 prefix_template；纯内存 PoC 保留内置默认。
	lookup := func(id string) (coherence.Template, bool) {
		if id == "t-default" {
			return coherence.Template{ID: id, Prefix: "2406::", Expr: "{v4.hextet4}"}, true
		}
		return coherence.Template{}, false
	}
	var tplAll func() []coherence.Template
	if pool != nil {
		tl := coherence.NewTplLoader(pool)
		if n, err := tl.Refresh(ctx); err != nil {
			log.Printf("template load: %v", err)
		} else {
			log.Printf("loaded %d templates from PG", n)
		}
		tl.StartRefreshLoop(ctx, 30*time.Second)
		lookup, tplAll = tl.Lookup, tl.All
	}

	stopSnap := coherence.StartSnapshotLoop(*snapshotPath, 5*time.Second, store.All)
	defer stopSnap()

	rec := coherence.NewReconciler(coherence.ExecController{}, *zone)
	go rec.Run(ctx, 30*time.Second, store.All)

	lis := listen(*sock)
	gs := grpc.NewServer()
	svc := coherence.NewService(store, lookup)
	if tplAll != nil {
		svc = svc.SetTemplateAll(tplAll)
	}
	coherencev1.RegisterCoherenceServer(gs, svc)
	log.Printf("coherence-daemon listening on %s (pg=%v)", *sock, *dsn != "")
	if err := gs.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

func listen(sock string) net.Listener {
	_ = os.Remove(sock) // 清理陈旧 socket
	lis, err := net.Listen("unix", sock)
	if err != nil {
		log.Fatalf("listen %s: %v", sock, err)
	}
	if err := os.Chmod(sock, 0o660); err != nil {
		log.Printf("chmod socket: %v", err)
	}
	return lis
}

// connectPG 启动全量加载 + NOTIFY 订阅（§2.3 一致性对账，K9）；返回池供模板装载复用。
func connectPG(ctx context.Context, dsn string, store coherence.Store) *pgxpool.Pool {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("pg pool: %v", err)
	}
	bindings, err := coherence.LoadAllBindings(ctx, pool)
	if err != nil {
		log.Fatalf("load bindings: %v", err)
	}
	for _, b := range bindings {
		store.Put(b)
	}
	log.Printf("loaded %d bindings from PG", len(bindings))

	go func() {
		if err := coherence.SubscribeNotify(ctx, pool, store); err != nil && ctx.Err() == nil {
			log.Printf("notify loop exit: %v", err)
		}
	}()
	return pool
}

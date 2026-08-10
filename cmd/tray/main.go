package main

import (
	"auto-router/internal/server"
	"fmt"
	"github.com/injoyai/goutil/oss"
	"github.com/injoyai/goutil/oss/tray"
	"github.com/injoyai/lorca"

	"net"
)

func main() {
	db := oss.UserInjoyDir("/auto-router/database/timer.db")
	// 系统自动分配空闲端口,tray 直接持有 listener 获知实际端口
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go timer.RunWithListener(ln, db)
	tray.Run(
		tray.WithIco(IcoTimer),
		tray.WithHint("定时任务"),
		func(s *tray.Tray) {
			x := s.AddMenu().SetName("配置").SetIcon(IcoMenuTimer)
			x.OnClick(func(m *tray.Menu) {
				lorca.Run(&lorca.Config{
					Width:  1080,
					Height: 940,
					Index:  fmt.Sprintf("http://localhost:%d", port),
				})
			})
		},
		tray.WithStartup(),
		tray.WithSeparator(),
		tray.WithExit(),
	)

	app := server.NewApp(cfg, st, key, gwToken, adminToken)
}

package main

import (
	"google.golang.org/grpc"
)

type App struct {
	server *grpc.Server
}

func NewApp(
	server *grpc.Server,
) *App {
	return &App{
		server: server,
	}
}

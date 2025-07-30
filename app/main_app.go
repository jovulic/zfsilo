package main

import "net/http"

type App struct {
	server *http.Server
}

func NewApp(
	server *http.Server,
) *App {
	return &App{
		server: server,
	}
}

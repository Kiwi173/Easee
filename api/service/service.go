package service

import "github.com/gorilla/mux"

// Httpd is the HTTP server interface
type Httpd interface {
	Router() *mux.Router
	Addr() string
	Version() string
	// JSON(interface{})
}

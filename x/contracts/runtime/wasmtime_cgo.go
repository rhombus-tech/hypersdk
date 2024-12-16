//go:build cgo
// +build cgo

package runtime

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo linux,amd64 LDFLAGS: -lwasmtime
#cgo darwin,amd64 LDFLAGS: -lwasmtime
#cgo windows,amd64 LDFLAGS: -lwasmtime
*/
import "C"
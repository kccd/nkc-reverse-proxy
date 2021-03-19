package proxy

import (
	"net/http"
	"strconv"
	"sync"

	"github.com/tokisakiyuu/nkc-proxy-go-pure/pkg/config"
)

type NKCProxy struct {
	Server    *http.Server
	ServerTLS *http.Server
	Config    config.Profile
}

func (p *NKCProxy) Launch() {
	ws := sync.WaitGroup{}
	p.ServerTLS.Addr = ":" + strconv.FormatInt(p.Config.Ports.HttpsPort, 10)
	p.Server.Addr = ":" + strconv.FormatInt(p.Config.Ports.HttpPort, 10)
	ws.Add(2)
	go func() {
		p.Server.ListenAndServe()
		ws.Done()
	}()
	go func() {
		p.ServerTLS.ListenAndServeTLS("", "")
		ws.Done()
	}()
	ws.Wait()
}

func NewNKCProxy(conf config.Profile) *NKCProxy {
	proxy := new(NKCProxy)
	proxy.Config = conf
	proxy.Server = NewServer(conf)
	proxy.ServerTLS = NewServer(conf)
	return proxy
}
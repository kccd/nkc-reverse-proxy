package modules

import (
	"crypto/tls"
	"errors"
	"github.com/dgryski/go-farm"
	"github.com/go-yaml/yaml"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

var proxyPassMap map[int64]map[string]*ProxyPass
var httpReverseProxy *httputil.ReverseProxy
var httpsReverseProxy *httputil.ReverseProxy

func GetConfigsPath() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	filePath := path.Join(root, "configs.yaml")
	if len(filePath) == 0 {
		return "", errors.New("未指定配置文件路径")
	}
	return filePath, nil
}

func GetConfigs() (*Configs, error) {
	configFilePath, err := GetConfigsPath()
	if err != nil {
		return nil, err
	}
	file, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}
	var configs Configs
	err = yaml.Unmarshal(file, &configs)
	if err != nil {
		return nil, err
	}
	return &configs, nil
}

func GetServersPortFromConfigs() (map[int64]*ServerPort, error) {
	configs, err := GetConfigs()
	if err != nil {
		return nil, err
	}
	serverPortMap := make(map[int64]*ServerPort)
	var tlsConfig *tls.Config
	for _, server := range configs.Servers {
		var tlsCFG *tls.Config
		if server.SSLKey != "" && server.SSLCert != "" {
			if tlsConfig == nil {
				tlsConfig, err = GetTLSConfig()
				if err != nil {
					return nil, err
				}
			}
			tlsCFG = tlsConfig
		}
		serverPort := serverPortMap[server.Listen]
		if serverPort == nil {
			serverPortMap[server.Listen] = &ServerPort{Port: server.Listen, TLSConfig: tlsCFG}
		} else if serverPort.TLSConfig != tlsCFG {
			return nil, errors.New("端口冲突：" + strconv.FormatInt(server.Listen, 10))
		}
	}
	return serverPortMap, nil
}

func GetTLSConfig() (*tls.Config, error) {
	cfg := tls.Config{}
	configs, err := GetConfigs()
	if err != nil {
		return nil, err
	}
	for _, server := range configs.Servers {
		if server.SSLKey == "" || server.SSLCert == "" {
			continue
		}
		cert, err := tls.LoadX509KeyPair(server.SSLCert, server.SSLKey)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = append(cfg.Certificates, cert)
	}
	return &cfg, nil
}

func GetProxyPassMap() (map[int64]map[string]*ProxyPass, error) {
	if proxyPassMap != nil {
		return proxyPassMap, nil
	}
	proxyPass := make(map[int64]map[string]*ProxyPass)
	configs, err := GetConfigs()
	if err != nil {
		return nil, err
	}
	for _, server := range configs.Servers {
		if proxyPass[server.Listen] == nil {
			proxyPass[server.Listen] = make(map[string]*ProxyPass)
		}
		for _, name := range server.Name {
			if proxyPass[server.Listen][name] == nil {
				var wsPass []string
				var wsType string
				if len(server.WSPass) > 0 {
					wsPass = server.WSPass
				} else {
					wsPass = server.WEBPass
				}
				if server.WSType == "" {
					wsType = server.WEBType
				} else {
					wsType = server.WSType
				}
				if server.RedirectUrl == "" && len(server.WEBPass) == 0 {
					return nil, errors.New("目标服务链接不能为空")
				}
				proxyPass[server.Listen][name] = &ProxyPass{
					WEBPass: server.WEBPass,
					WSPass:  wsPass,
					WEBType: server.WEBType,
					WSType:  wsType,
					Redirect: RedirectInfo{
						Code: server.RedirectCode,
						Url:  server.RedirectUrl,
					},
				}
			}
		}
	}
	proxyPassMap = proxyPass
	return proxyPassMap, nil
}

func GetClientIP(r *http.Request) string {
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	ip := strings.TrimSpace(strings.Split(xForwardedFor, ",")[0])
	if ip != "" {
		return ip
	}

	ip = strings.TrimSpace(r.Header.Get("X-Real-Ip"))
	if ip != "" {
		return ip
	}

	if ip, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return ip
	}

	return ""
}

func GetUrlByPassType(pass []string, passType string, ip string) string {
	var index uint64
	passCount := len(pass)
	if passCount == 1 {
		index = 0
	} else if passType == "random" {
		index = uint64(rand.Intn(passCount))
	} else {
		bytes := []byte(ip)
		hash := farm.Hash64(bytes)
		index = hash % uint64(passCount)
	}
	return pass[index]
}

func GetTargetPassInfo(req *http.Request, isHttps bool) (*url.URL, *RedirectInfo, error) {
	proxyPassMap, err := GetProxyPassMap()
	if err != nil {
		return nil, nil, err
	}
	hostInfo := strings.Split(req.Host, ":")
	if len(hostInfo) == 0 {
		return nil, nil, errors.New("客户端未指定 host")
	}
	host := hostInfo[0]
	polling := req.Header.Get("X-socket-io")
	isWS := polling == "polling"
	var port int64
	var errInfo error
	if len(hostInfo) == 1 {
		if isHttps {
			port = 443
		} else {
			port = 80
		}
	} else {
		port, errInfo = strconv.ParseInt(hostInfo[1], 10, 64)
		if errInfo != nil {
			return nil, nil, errInfo
		}
	}

	if err != nil {
		return nil, nil, err
	}
	var pass []string
	var passType string

	proxyPass := proxyPassMap[port][host]
	if proxyPass == nil {
		return nil, nil, nil
	}

	if isWS {
		pass = proxyPass.WSPass
		passType = proxyPass.WSType
	} else {
		pass = proxyPass.WEBPass
		passType = proxyPass.WEBType
	}

	var urlInfo *url.URL

	if len(pass) > 0 {
		ip := GetClientIP(req)
		targetUrlString := GetUrlByPassType(pass, passType, ip)
		var err error
		urlInfo, err = url.Parse(targetUrlString)
		if err != nil {
			return nil, nil, err
		}
	}

	return urlInfo, &proxyPass.Redirect, nil
}
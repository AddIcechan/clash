package clash

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#import "UIHelper.h"
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"clash/config"

	"clash/constant"

	"clash/hub/executor"

	"clash/hub/route"

	"clash/log"

	"clash/tunnel/statistic"

	"github.com/oschwald/geoip2-golang"
	"github.com/phayes/freeport"
)

var secretOverride string = ""

func IsAddrValid(addr string) bool {
	if addr != "" {
		comps := strings.Split(addr, ":")
		v := comps[len(comps)-1]
		if port, err := strconv.Atoi(v); err == nil {
			if port > 0 && port < 65535 {
				return CheckPortAvailable(port)
			}
		}
	}
	return false
}

func CheckPortAvailable(port int) bool {
	if port < 1 || port > 65534 {
		return false
	}
	addr := ":"
	l, err := net.Listen("tcp", addr+strconv.Itoa(port))
	if err != nil {
		log.Warnln("check port fail 0.0.0.0:%d", port)
		return false
	}
	_ = l.Close()

	addr = "127.0.0.1:"
	l, err = net.Listen("tcp", addr+strconv.Itoa(port))
	if err != nil {
		log.Warnln("check port fail 127.0.0.1:%d", port)
		return false
	}
	_ = l.Close()
	log.Infoln("check port %d success", port)
	return true
}

//export InitClashCore
func InitClashCore() {
	configFile := filepath.Join(constant.Path.HomeDir(), constant.Path.Config())
	constant.SetConfig(configFile)
}

func ReadConfig(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, err
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("Configuration file %s is empty", path)
	}
	return data, err
}

func GetRawCfgByPath(path string) (*config.RawConfig, error) {
	if len(path) == 0 {
		path = constant.Path.Config()
	}

	buf, err := ReadConfig(path)
	if err != nil {
		return nil, err
	}
	return config.UnmarshalRawConfig(buf)
}

func GetRawCfg() (*config.RawConfig, error) {

	buf, err := ReadConfig(constant.Path.Config())
	if err != nil {
		return nil, err
	}

	return config.UnmarshalRawConfig(buf)
}

func SetupHomeDir(homeDirPath string) {
	constant.SetHomeDir(homeDirPath)
}

func RunByConfig(configPath string, externalController string) error {
	log.Infoln("start run")
	constant.SetConfig(configPath)
	rawConfig, err := GetRawCfgByPath(configPath)
	if err != nil {
		return err
	}
	log.Infoln("current rawconfig mixedPort: %d", rawConfig.MixedPort)
	log.Infoln("current rawconfig mode: %d", rawConfig.Mode)
	rawConfig.ExternalUI = ""
	rawConfig.Profile.StoreSelected = false
	if len(externalController) > 0 {
		rawConfig.ExternalController = externalController
	}

	cfg, err := config.ParseRawConfig(rawConfig)
	if err != nil {
		log.Infoln("config.parse raw config failed by error: %s", err.Error())
		return err
	}
	go route.Start(cfg.General.ExternalController, cfg.General.Secret)
	executor.ApplyConfig(cfg, true)
	log.Infoln("apply config success")
	return nil

}

func ParseDefaultConfigThenStart(checkPort, allowLan bool, proxyPort uint32, externalController string) (*config.Config, error) {
	rawCfg, err := GetRawCfg()
	if err != nil {
		return nil, err
	}

	if proxyPort > 0 {
		rawCfg.MixedPort = int(proxyPort)
		if rawCfg.Port == rawCfg.MixedPort {
			rawCfg.Port = 0
		}
		if rawCfg.SocksPort == rawCfg.MixedPort {
			rawCfg.SocksPort = 0
		}
	} else {
		if rawCfg.MixedPort == 0 {
			if rawCfg.Port > 0 {
				rawCfg.MixedPort = rawCfg.Port
				rawCfg.Port = 0
			} else if rawCfg.SocksPort > 0 {
				rawCfg.MixedPort = rawCfg.SocksPort
				rawCfg.SocksPort = 0
			} else {
				rawCfg.MixedPort = 7890
			}

			if rawCfg.SocksPort == rawCfg.MixedPort {
				rawCfg.SocksPort = 0
			}

			if rawCfg.Port == rawCfg.MixedPort {
				rawCfg.Port = 0
			}
		}
	}
	if secretOverride != "" {
		rawCfg.Secret = secretOverride
	}
	rawCfg.ExternalUI = ""
	rawCfg.Profile.StoreSelected = false
	if len(externalController) > 0 {
		rawCfg.ExternalController = externalController
	}
	if checkPort {
		if !IsAddrValid(rawCfg.ExternalController) {
			port, err := freeport.GetFreePort()
			if err != nil {
				return nil, err
			}
			rawCfg.ExternalController = "127.0.0.1:" + strconv.Itoa(port)
			rawCfg.Secret = ""
		}
		rawCfg.AllowLan = allowLan

		if !CheckPortAvailable(rawCfg.MixedPort) {
			if port, err := freeport.GetFreePort(); err == nil {
				rawCfg.MixedPort = port
			}
		}
	}

	cfg, err := config.ParseRawConfig(rawCfg)
	if err != nil {
		return nil, err
	}
	go route.Start(cfg.General.ExternalController, cfg.General.Secret)
	executor.ApplyConfig(cfg, true)
	return cfg, nil
}

//export VerifyClashConfig
func VerifyClashConfig(content *C.char) *C.char {

	b := []byte(C.GoString(content))
	cfg, err := executor.ParseWithBytes(b)
	if err != nil {
		return C.CString(err.Error())
	}

	if len(cfg.Proxies) < 1 {
		return C.CString("No proxy found in config")
	}
	return C.CString("success")
}

//export ClashSetupLogger
func ClashSetupLogger() {
	sub := log.Subscribe()
	go func() {
		for elm := range sub {
			log := elm.(log.Event)
			cs := C.CString(log.Payload)
			cl := C.CString(log.Type())
			C.sendLogToUI(cs, cl)
			C.free(unsafe.Pointer(cs))
			C.free(unsafe.Pointer(cl))
		}
	}()
}

//export ClashSetupTraffic
func ClashSetupTraffic() {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		t := statistic.DefaultManager
		buf := &bytes.Buffer{}
		for range tick.C {
			buf.Reset()
			up, down := t.Now()
			C.sendTrafficToUI(C.longlong(up), C.longlong(down))
		}
	}()
}

//export Clash_checkSecret
func Clash_checkSecret() *C.char {
	cfg, err := GetRawCfg()
	if err != nil {
		return C.CString("")
	}
	if cfg.Secret != "" {
		return C.CString(cfg.Secret)
	}
	return C.CString("")
}

//export Clash_setSecret
func Clash_setSecret(secret *C.char) {
	secretOverride = C.GoString(secret)
}

//export Run
func Run(checkConfig, allowLan bool, portOverride uint32, externalController *C.char) *C.char {
	cfg, err := ParseDefaultConfigThenStart(checkConfig, allowLan, portOverride, C.GoString(externalController))
	if err != nil {
		return C.CString(err.Error())
	}

	portInfo := map[string]string{
		"externalController": cfg.General.ExternalController,
		"secret":             cfg.General.Secret,
	}

	jsonString, err := json.Marshal(portInfo)
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString(string(jsonString))
}

//export SetUIPath
func SetUIPath(path *C.char) {
	route.SetUIPath(C.GoString(path))
}

//export ClashUpdateConfig
func ClashUpdateConfig(path *C.char) *C.char {
	cfg, err := executor.ParseWithPath(C.GoString(path))
	if err != nil {
		return C.CString(err.Error())
	}
	executor.ApplyConfig(cfg, false)
	return C.CString("success")
}

//export ClashGetConfigs
func ClashGetConfigs() *C.char {
	general := executor.GetGeneral()
	jsonString, err := json.Marshal(general)
	if err != nil {
		return C.CString(err.Error())
	}
	return C.CString(string(jsonString))
}

//export VerifyGEOIPDataBase
func VerifyGEOIPDataBase() bool {
	mmdb, err := geoip2.Open(constant.Path.MMDB())
	if err != nil {
		log.Warnln("mmdb fail:%s", err.Error())
		return false
	}

	_, err = mmdb.Country(net.ParseIP("114.114.114.114"))
	if err != nil {
		log.Warnln("mmdb lookup fail:%s", err.Error())
		return false
	}
	return true
}

//export Clash_closeAllConnections
func Clash_closeAllConnections() {
	snapshot := statistic.DefaultManager.Snapshot()
	for _, c := range snapshot.Connections {
		c.Close()
	}
}

func CheckLog() {
	log.LevelCheck()
}

func CustomLogFile(logPath string) {
	log.CustomLog(logPath)
}

func main() {
	fmt.Printf("main build")
	log.CustomLog("/Users/addice/Library/Containers/com.addice.Demo.macOS-PacketTunnel/Data/Documents/tun2socks")
	log.Debugln("this is debug from main")
	log.Infoln("this is debug from main")
	log.Errorln("this is debug from main")
	log.Warnln("this is debug from main")
}

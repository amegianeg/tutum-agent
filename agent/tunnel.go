package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ActiveState/tail"
	"github.com/tutumcloud/tutum-agent/utils"
)

type TunnelPatchForm struct {
	Tunnel  string `json:"tunnel"`
	Version string `json:"agent_version"`
}

func NatTunnel(url, ngrokPath, ngrokLogPath, ngrokConfPath, ip string) {
	if !utils.FileExist(ngrokPath) {
		Logger.Printf("Cannot find NAT tunnel binary (%s)", ngrokPath)
		return
	}

	if !isNodeNated(ip) {
		return
	}

	updateNgrokHost(url)
	createNgrokConfFile(ngrokConfPath)

	var cmd *exec.Cmd
	if *FlagNgrokToken != "" {
		cmd = exec.Command(ngrokPath,
			"-log", "stdout",
			"-authtoken", *FlagNgrokToken,
			"-proto", "tcp",
			DockerHostPort)
	} else {
		if !utils.FileExist(ngrokConfPath) {
			SendError(errors.New("Cannot find ngrok conf"), "Cannot find ngrok conf file", nil)
			Logger.Println("Cannot find NAT tunnel configuration")
			return
		}
		cmd = exec.Command(ngrokPath,
			"-config", ngrokConfPath,
			"-log", "stdout",
			"-proto", "tcp",
			DockerHostPort)
	}

	os.RemoveAll(ngrokLogPath)
	logFile, err := os.OpenFile(ngrokLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		SendError(err, "Failed to open ngrok log file", nil)
		Logger.Println(err)
	} else {
		defer logFile.Close()
		cmd.Stdout = logFile
	}

	go monitorTunnels(url, ngrokLogPath)
	Logger.Println("Starting NAT tunnel:", cmd.Args)

	for {
		runGronk(cmd)
		time.Sleep(10 * time.Second)
		Logger.Println("Restarting NAT tunnel:", cmd.Args)
	}
}

func runGronk(cmd *exec.Cmd) {
	if err := cmd.Start(); err != nil {
		SendError(err, "Failed to run NAT tunnel", nil)
		Logger.Println(err)
		return
	}
	cmd.Wait()
}

func monitorTunnels(url, ngrokLogPath string) {
	update, _ := tail.TailFile(ngrokLogPath, tail.Config{
		Follow: true,
		ReOpen: true})
	for line := range update.Lines {
		if strings.Contains(line.Text, "[INFO] [client] Tunnel established at") {
			terms := strings.Split(line.Text, " ")
			tunnel := terms[len(terms)-1]
			Logger.Printf("Found new tunnel: %s", tunnel)
			if tunnel != "" {
				patchTunnelToTutum(url, tunnel)
			}
		}
	}
}

func patchTunnelToTutum(url, tunnel string) {
	Logger.Println("Sending tunnel address to Tutum")
	form := TunnelPatchForm{}
	form.Version = VERSION
	form.Tunnel = tunnel
	data, err := json.Marshal(form)
	if err != nil {
		SendError(err, "Json marshal error", nil)
		Logger.Printf("Cannot marshal the TunnelPatch form:%s\f", err)
	}

	headers := []string{"Authorization TutumAgentToken " + Conf.TutumToken,
		"Content-Type", "application/json"}
	_, err = SendRequest("PATCH", utils.JoinURL(url, Conf.TutumUUID), data, headers)
	if err != nil {
		SendError(err, "Failed to patch tunnel address to Tutum", nil)
		Logger.Println("Failed to patch tunnel address to Tutum,", err)
	}
}

func DownloadNgrok(url, ngrokBinPath string) {
	if !utils.FileExist(ngrokBinPath) {
		Logger.Println("Downloading NAT tunnel binary...")
		downloadFile(url, ngrokBinPath, "ngrok")
	}
}

func createNgrokConfFile(ngrokConfPath string) {
	ngrokConfStr := fmt.Sprintf("server_addr: %s\ntrust_host_root_certs: false", NgrokHost)
	if err := ioutil.WriteFile(ngrokConfPath, []byte(ngrokConfStr), 0666); err != nil {
		SendError(err, "Failed to create ngrok config file", nil)
		Logger.Println("Cannot create ngrok config file:", err)
	}
}

func updateNgrokHost(url string) {
	if NgrokHost != "" {
		return
	}

	headers := []string{"Authorization TutumAgentToken " + Conf.TutumToken,
		"Content-Type application/json"}
	body, err := SendRequest("GET", utils.JoinURL(url, Conf.TutumUUID), nil, headers)
	if err != nil {
		SendError(err, "SendRequest error", nil)
		Logger.Printf("Get registration info error, %s", err)
	} else {
		var form RegGetForm
		if err = json.Unmarshal(body, &form); err != nil {
			SendError(err, "Json unmarshal error", nil)
			Logger.Println("Cannot unmarshal the response", err)
		} else {
			if form.NgrokHost != "" {
				NgrokHost = form.NgrokHost
				Logger.Println("Tunnel address:", NgrokHost)
			}
		}
	}
}

func isNodeNated(ip string) bool {
	for {
		_, err := net.Dial("tcp", fmt.Sprintf("%s:%s", "localhost", DockerHostPort))
		if err == nil {
			break
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	address := ip
	if address == "" {
		Logger.Printf("Node public IP address return from server is empty, use FQDN instead.")
		address = Conf.CertCommonName
	}
	Logger.Printf("Testing if node %s(%s:%s) is publicly reachable...", Conf.CertCommonName, address, DockerHostPort)
	_, err := net.Dial("tcp", fmt.Sprintf("%s:%s", address, DockerHostPort))
	if err == nil {
		Logger.Printf("Node %s(%s:%s) is publicly reachable", Conf.CertCommonName, address, DockerHostPort)
		return false
	} else {
		Logger.Printf("Node %s(%s:%s) is not publicly reachable: %s", Conf.CertCommonName, address, DockerHostPort, err)
		return true
	}
}

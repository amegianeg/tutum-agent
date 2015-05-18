package agent

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/tutumcloud/tutum-agent/utils"
)

type RegResponseForm struct {
	UserCaCert      string `json:"user_ca_cert"`
	TutumUUID       string `json:"uuid"`
	CertCommonName  string `json:"external_fqdn"`
	DockerBinaryURL string `json:"docker_url"`
	NgrokBinaryURL  string `json:"ngrok_url"`
	PublicIpAddress string `json:"public_ip"`
}

type RegPostForm struct {
	Version string `json:"agent_version"`
}

type RegPatchForm struct {
	Public_cert string `json:"public_cert"`
	Version     string `json:"agent_version"`
}

type RegGetForm struct {
	AgentVersion string `json:"agent_version"`
	DockerUrl    string `json:"docker_url"`
	ExternalFqdn string `json:"external_fqdn"`
	NgrokUrl     string `json:"ngrok_url"`
	PublicCert   string `json:"public_cert"`
	ResourceUri  string `json:"resource_uri"`
	State        string `json:"state"`
	Tunnel       string `json:"tunnel"`
	UserCaCert   string `json:"user_ca_cert"`
	UUID         string `json:"uuid"`
	NgrokHost    string `json:"ngrok_server_addr"`
}

func PostToTutum(url, caFilePath, configFilePath string) error {
	form := RegPostForm{}
	form.Version = VERSION
	data, err := json.Marshal(form)
	if err != nil {
		SendError(err, "Fatal: Json marshal error", nil)
		Logger.Fatalln("Cannot marshal the POST form", err)
	}
	return register(url, "POST", Conf.TutumToken, Conf.TutumUUID, caFilePath, configFilePath, data)
}

func PatchToTutum(url, caFilePath, certFilePath, configFilePath string) error {
	form := RegPatchForm{}
	form.Version = VERSION
	cert, err := GetCertificate(certFilePath)
	if err != nil {
		SendError(err, "Fatal: Failed to load public certificate", nil)
		Logger.Fatal("Cannot read public certificate:", err)
	}
	form.Public_cert = *cert
	data, err := json.Marshal(form)
	if err != nil {
		SendError(err, "Fatal: Json marshal error", nil)
		Logger.Fatalln("Cannot marshal the PATCH form", err)
	}

	return register(url, "PATCH", Conf.TutumToken, Conf.TutumUUID, caFilePath, configFilePath, data)
}

func VerifyRegistration(url string) {
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
			if form.State == "Deployed" {
				Logger.Println("Node registration successful with", Conf.TutumHost)
				return
			}
		}
	}

	time.Sleep(5 * time.Minute)

	body, err = SendRequest("GET", utils.JoinURL(url, Conf.TutumUUID), nil, headers)
	if err != nil {
		SendError(err, "Failed to get registration info after 5 minutes", nil)
		Logger.Printf("Get registration info error, %s", err)
	} else {
		var form RegGetForm
		if err = json.Unmarshal(body, &form); err != nil {
			SendError(err, "Json unmarshal error", nil)
			Logger.Println("Cannot unmarshal the response", err)
		} else {
			if form.State == "Deployed" {
				Logger.Println("Node registration successful with", Conf.TutumHost)
			} else {
				Logger.Println("Node registration timed out with", Conf.TutumHost)
				Logger.Println("Node state:", form.State)
			}
		}
	}
}

func register(url, method, token, uuid, caFilePath, configFilePath string, data []byte) error {
	if token == "" {
		fmt.Fprintf(os.Stderr, "Tutum token is empty. Please run 'tutum-agent set TutumToken=xxx' first!\n")
		Logger.Fatalln("Tutum token is empty. Please run 'tutum-agent set TutumToken=xxx' first!")
	}

	for i := 1; ; i *= 2 {
		if i > MaxWaitingTime {
			i = 1
		}
		body, err := sendRegRequest(url, method, token, uuid, data)
		if err == nil {
			if err = handleRegResponse(body, caFilePath, configFilePath); err == nil {
				return nil
			} else {
				Logger.Printf("Failed to handle the registration response, %s. Retry in %d seconds", err, i)
				time.Sleep(time.Duration(i) * time.Second)
				continue
			}
		}
		if method == "POST" && (err.Error() == "401") {
			SendError(err, "Registration unauthorized: POST", nil)
			Logger.Fatalln("Cannot register node in Tutum: unauthorized. Please try again with a new Tutum token.")
		}
		if method == "PATCH" && (err.Error() == "404" || err.Error() == "401") {
			return err
		}
		SendError(err, "Registration HTTP error", nil)
		Logger.Printf("Registration failed, %s. Retry in %d seconds", err, i)
		time.Sleep(time.Duration(i) * time.Second)
	}
}

func sendRegRequest(url, method, token, uuid string, data []byte) ([]byte, error) {
	headers := []string{"Authorization TutumAgentToken " + token,
		"Content-Type application/json"}
	return SendRequest(method, utils.JoinURL(url, uuid), data, headers)

}

func handleRegResponse(body []byte, caFilePath, configFilePath string) error {
	var responseForm RegResponseForm

	// Save user ca cert file
	if err := json.Unmarshal(body, &responseForm); err != nil {
		SendError(err, "Json unmarshal error", nil)
		Logger.Println("Failed to unmarshal the response", err)
		return err
	}
	if err := ioutil.WriteFile(caFilePath, []byte(responseForm.UserCaCert), 0644); err != nil {
		SendError(err, "Failed to save user ca cert file", nil)
		Logger.Println("Failed to save", caFilePath, err)
		return err
	}
	// Update global Conf
	isModified := false
	if Conf.CertCommonName != responseForm.CertCommonName {
		Logger.Printf("Cert CommonName has been changed from %s to %s", Conf.CertCommonName, responseForm.CertCommonName)
		isModified = true
		Conf.CertCommonName = responseForm.CertCommonName
	}
	if Conf.TutumUUID != responseForm.TutumUUID {
		Logger.Printf("Tutum UUID has been changed from %s to %s", Conf.TutumUUID, responseForm.TutumUUID)
		isModified = true
		Conf.TutumUUID = responseForm.TutumUUID
	}

	DockerBinaryURL = responseForm.DockerBinaryURL
	NodePublicIp = responseForm.PublicIpAddress

	if responseForm.NgrokBinaryURL != "" {
		NgrokBinaryURL = responseForm.NgrokBinaryURL
	}
	// Save to configuration file
	if isModified {
		Logger.Println("Updating configuration file...")
		return SaveConf(configFilePath, Conf)
	}
	return nil
}
